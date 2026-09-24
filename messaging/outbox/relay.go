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

const (
	// DefaultPoll is how long an idle relay waits before its next pass.
	DefaultPoll = time.Second
	// DefaultTimeout bounds the handling of one row.
	DefaultTimeout = 10 * time.Second
)

// RelayOption configures a Relay.
type RelayOption func(*Relay)

// Poll sets how long the relay waits between passes once a pass finds no
// row, or ends on a failure. It must be positive.
func Poll(d time.Duration) RelayOption {
	return func(r *Relay) { r.poll = d }
}

// Timeout sets the deadline on each row's handler context. The row stays
// locked while its handler runs, so the timeout bounds how long a hung
// publish holds it. It must be positive.
func Timeout(d time.Duration) RelayOption {
	return func(r *Relay) { r.timeout = d }
}

// Relay is the source of the outbox's committed, unpublished events. A
// composition root runs it into a reactor whose handler publishes each
// event to the broker.
//
// Each row is handled in a transaction of its own. The relay locks the
// oldest unpublished row, skipping rows another relay holds, and calls the
// handler with its event. When the handler returns nil, the row is marked
// published in the same transaction. When it returns an error, including
// one marked [event.Permanent], the transaction rolls back, the row stays
// unpublished, and the pass ends. The next pass, a poll later, retries it.
// A stop anywhere before the commit leaves the row to be published again
// under the same id, so delivery is at least once and a broker that
// deduplicates on the id sees it once.
//
// One relay publishes in emission order and holds the rest of the outbox
// back behind a row that fails. Several relays share the rows and publish
// each one once, but not in order.
type Relay struct {
	o       *Outbox
	db      sqlate.Beginner
	poll    time.Duration
	timeout time.Duration
	ready   atomic.Bool
}

var _ reactor.Source[event.Event] = (*Relay)(nil)

func newRelay(o *Outbox, db sqlate.Beginner, opts ...RelayOption) *Relay {
	r := &Relay{o: o, db: db, poll: DefaultPoll, timeout: DefaultTimeout}
	for _, opt := range opts {
		opt(r)
	}
	if r.poll <= 0 || r.timeout <= 0 {
		panic("outbox: relay poll and timeout must be positive")
	}
	return r
}

// errCorrupt marks a row that can never become an event.
var errCorrupt = errors.New("corrupt row")

// Receive publishes through fn until ctx ends. A handler error or a
// database error ends the pass, not Receive; a database error also makes
// the relay not ready until a pass succeeds. Receive fails only on a row
// that cannot be decoded, which would otherwise hold the outbox back
// forever. A row whose handler is running when ctx ends is settled before
// Receive returns.
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
	settle := context.WithoutCancel(ctx)
	tx, err := r.db.Begin(settle)
	if err != nil {
		return false, err
	}
	defer func() {
		if !handled || err != nil {
			_ = tx.Rollback()
		}
	}()

	claimed, err := r.o.eng.ClaimRow.Scan(scanRow).One(ctx, tx, nil)
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

	hctx, cancel := context.WithTimeout(settle, r.timeout)
	err = fn(hctx, e)
	cancel()
	if err != nil {
		return false, handlerError{fmt.Errorf("outbox: relay: event %s: %w", e.ID, err)}
	}
	if _, err := r.o.eng.MarkPublished.Exec(settle, tx, query.Args{"seq": seq}); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// Ready reports whether the relay is receiving and its last pass reached
// the database.
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
