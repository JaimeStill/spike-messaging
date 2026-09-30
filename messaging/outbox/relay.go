package outbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/query"

	"github.com/JaimeStill/spike-messaging/core/event"
	"github.com/JaimeStill/spike-messaging/core/reactor"
)

// defaultTimeout bounds the handling of one row unless [Timeout] sets it.
const defaultTimeout = 10 * time.Second

// RelayOption configures a Relay.
type RelayOption func(*Relay)

// Timeout sets the deadline on each row's handler context. The row stays
// locked while its handler runs, so the timeout bounds how long a hung
// publish holds it. The row's whole transaction is bounded at twice the
// timeout, so a stalled database, or a handler that ignores its deadline,
// rolls the transaction back rather than hold the row and the drain. It
// must be positive.
func Timeout(d time.Duration) RelayOption {
	return func(r *Relay) { r.timeout = d }
}

// Drain sets how long a relay keeps publishing once it is cancelled. The
// relay makes one last pass on a context of its own, bounded by d, and
// returns when the outbox is empty or a row fails, so the events committed
// while the producers drained are published before the process exits rather
// than at its next start. d bounds the claims: once it runs out, the pass
// claims no further row. A row already claimed is settled on its own
// deadlines, its handler's [Timeout] and its transaction's twice that, as
// every row is, unless the reactor's [reactor.Grace] cancels its handler
// first, so set d below the Grace. The default, 0, makes no last pass. It
// must not be negative.
func Drain(d time.Duration) RelayOption {
	return func(r *Relay) { r.drain = d }
}

// Relay is the source of the outbox's committed, unpublished events. A
// composition root runs it into a reactor whose handler publishes each
// event to the broker.
//
// Each row is handled in a transaction of its own. The relay locks the
// oldest unpublished row, skipping rows another relay holds, and calls the
// handler with its event inside that transaction, so the row stays locked
// while the event is published. When the handler returns nil, the row is
// marked published in the same transaction. When it returns an error, including
// one marked [event.Permanent], the transaction rolls back, the row stays
// unpublished, and the pass ends. The next pass, a poll later, retries it.
// A stop anywhere before the commit leaves the row to be published again
// under the same source and id, so delivery is at least once and a broker
// that deduplicates on them sees it once.
//
// One relay publishes in seq order, which is the order the rows were
// inserted among those committed, not the order their transactions
// committed: a long transaction can commit a row after a later one was
// published. It holds the rest of the outbox back behind a row that fails.
// Several relays share the rows, and each row is claimed by one relay at a
// time, but they publish in no particular order.
type Relay struct {
	eng     Engine
	db      sqlate.Beginner
	poll    time.Duration
	timeout time.Duration
	drain   time.Duration
	ready   atomic.Bool
}

var _ reactor.Source[event.Event] = (*Relay)(nil)

// errCorrupt marks a row that can never become an event.
var errCorrupt = errors.New("corrupt row")

// Receive publishes through fn until ctx ends. A handler error or a
// database error ends the pass, not Receive; a database error also makes
// the relay not ready until a pass succeeds. Receive fails only on a row
// that cannot be decoded, which would otherwise hold the outbox back
// forever. A row whose handler is running when ctx ends is settled before
// Receive returns, and a relay with a [Drain] then makes its last pass.
func (r *Relay) Receive(ctx context.Context, fn reactor.Func[event.Event]) error {
	r.ready.Store(true)
	defer r.ready.Store(false)
	for ctx.Err() == nil {
		err := r.pass(ctx, fn)
		switch {
		case errors.Is(err, errCorrupt):
			return err
		case err == nil, ctx.Err() != nil:
			r.ready.Store(true)
		default:
			var handler handlerError
			r.ready.Store(errors.As(err, &handler))
		}
		t := time.NewTimer(r.poll)
		select {
		case <-ctx.Done():
		case <-t.C:
		}
		t.Stop()
	}
	if r.drain > 0 {
		// The last pass's failure leaves its row for the next start, as any
		// failed pass does, so it is not Receive's error.
		dctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), r.drain)
		defer cancel()
		_ = r.pass(dctx, fn)
	}
	return nil
}

// handlerError is a pass that ended on the handler's error.
type handlerError struct{ error }

// pass handles rows until none is left, returning nil, or until one fails.
func (r *Relay) pass(ctx context.Context, fn reactor.Func[event.Event]) error {
	for ctx.Err() == nil {
		handled, err := r.next(ctx, fn)
		if err != nil || !handled {
			return err
		}
	}
	return nil
}

// next handles the oldest unclaimed row, reporting whether there was one.
func (r *Relay) next(ctx context.Context, fn reactor.Func[event.Event]) (handled bool, err error) {
	// database/sql rolls a transaction back when the context it began with
	// ends, so the transaction begins on a context that outlives ctx: a row
	// claimed before ctx ends is settled, and its handler keeps only its own
	// deadline. Only the claim itself stops at ctx.
	// The transaction's own deadline, twice the handler's, bounds a stalled
	// database or a handler that ignores its deadline.
	settle, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*r.timeout)
	defer cancel()
	tx, err := r.db.Begin(settle)
	if err != nil {
		return false, err
	}
	defer func() {
		if !handled || err != nil {
			_ = tx.Rollback()
		}
	}()

	claimed, err := r.eng.ClaimRow.Scan(scanRow).One(ctx, tx, nil)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	seq := claimed.seq

	var h event.Header
	if err := json.Unmarshal(claimed.header, &h); err != nil {
		return false, fmt.Errorf("outbox: relay: row %d: %w: %w", seq, errCorrupt, err)
	}
	e, err := event.Decode(h, claimed.data)
	if err != nil {
		return false, fmt.Errorf("outbox: relay: row %d: %w: %w", seq, errCorrupt, err)
	}

	hctx, cancelHandler := context.WithTimeout(settle, r.timeout)
	err = fn(hctx, e)
	cancelHandler()
	if err != nil {
		return false, handlerError{fmt.Errorf("outbox: relay: event %s: %w", e.ID, err)}
	}
	n, err := r.eng.MarkPublished.Exec(settle, tx, query.Args{"seq": seq})
	if err != nil {
		return false, err
	}
	if n != 1 {
		// An engine whose mark changes no row would have the relay publish
		// the row again on every pass.
		return false, fmt.Errorf("outbox: relay: marking row %d published changed %d rows, want 1", seq, n)
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// Ready reports whether the relay is receiving and no pass since its last
// successful one has failed to reach the database. It is true from the
// start of Receive, before the first pass. A handler's error, such as a
// broker that is down, leaves the relay ready: the row waits for the next
// pass, and readiness speaks only for the database.
func (r *Relay) Ready() bool { return r.ready.Load() }

// row is one claimed outbox row.
type row struct {
	seq    int64
	header []byte
	data   []byte
}

// scanRow reads ClaimRow's columns in the order [Engine] fixes.
func scanRow(r query.Row) (row, error) {
	var v row
	err := r.Scan(&v.seq, &v.header, &v.data)
	return v, err
}
