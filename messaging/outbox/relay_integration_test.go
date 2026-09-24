//go:build integration

package outbox_test

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/standards-lab/sqlate"

	"github.com/JaimeStill/spike-messaging/core/event"
	"github.com/JaimeStill/spike-messaging/core/reactor"
	"github.com/JaimeStill/spike-messaging/messaging"
	"github.com/JaimeStill/spike-messaging/messaging/memory"
	"github.com/JaimeStill/spike-messaging/messaging/outbox"
)

const (
	poll     = 20 * time.Millisecond
	failsafe = 10 * time.Second
)

// recorder keeps the ids of the events a handler saw, in order.
type recorder struct {
	mu  sync.Mutex
	ids []string
}

func (r *recorder) add(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ids = append(r.ids, id)
}

func (r *recorder) seen() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.ids)
}

func (r *recorder) record(_ context.Context, e event.Event) error {
	r.add(e.ID)
	return nil
}

// relay starts a reactor over a new relay on db and shuts it down when the
// test ends, unless the test stops it first.
func relay(t *testing.T, db *sqlate.DB, fn reactor.Func[event.Event], opts ...reactor.Option) *reactor.Reactor[event.Event] {
	t.Helper()
	r := reactor.New(outbox.NewRelay(db, outbox.Poll(poll)), fn, opts...)
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stop(r) })
	return r
}

// observe subscribes a recorder to b, so a test sees what the relay
// published.
func observe(t *testing.T, b *memory.Broker) *recorder {
	t.Helper()
	src, err := b.Subscribe(messaging.Subscription{Name: "observer"})
	if err != nil {
		t.Fatal(err)
	}
	rec := &recorder{}
	r := reactor.New(src, rec.record)
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stop(r) })
	return rec
}

// stop shuts r down within the failsafe, so a failing test's cleanup
// cannot hang on a handler that never returns.
func stop(r *reactor.Reactor[event.Event]) {
	ctx, cancel := context.WithTimeout(context.Background(), failsafe)
	defer cancel()
	_ = r.Shutdown(ctx)
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(failsafe)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func ids(from, to int) []string {
	var out []string
	for n := from; n <= to; n++ {
		out = append(out, strconv.Itoa(n))
	}
	return out
}

func emitEach(t *testing.T, db *sqlate.DB, idList ...string) {
	t.Helper()
	for _, id := range idList {
		emit(t, db, tick(id))
	}
}

func drained(t *testing.T, db *sqlate.DB) func() bool {
	return func() bool {
		_, pending := rows(t, db)
		return pending == 0
	}
}

func TestRelayPublishesInEmissionOrder(t *testing.T) {
	db := migrated(t)
	emitEach(t, db, ids(1, 5)...)
	rec := &recorder{}
	r := relay(t, db, rec.record)
	eventually(t, "five events", func() bool { return len(rec.seen()) == 5 })
	if got := rec.seen(); !slices.Equal(got, ids(1, 5)) {
		t.Fatalf("published %v, want %v", got, ids(1, 5))
	}
	if !r.Ready() {
		t.Error("a running relay is not ready")
	}
	eventually(t, "no pending rows", drained(t, db))
}

// Evidence 2: a stop between commit and publish loses no event. Events 1 and
// 2 are committed while no relay runs. A first relay is stopped with event 3
// in flight, its publish cancelled after the grace. Event 4 is committed
// while no relay runs again. A second relay then publishes all four.
func TestStopBetweenCommitAndPublishLosesNoEvent(t *testing.T) {
	db := migrated(t)
	b := memory.New()
	rec := observe(t, b)

	emitEach(t, db, "1", "2", "3")
	entered := make(chan struct{})
	first := relay(t, db, func(ctx context.Context, e event.Event) error {
		if e.ID == "3" {
			close(entered)
			<-ctx.Done() // a publish that never completes
			return ctx.Err()
		}
		return b.Publish(ctx, e)
	}, reactor.Grace(50*time.Millisecond))
	<-entered
	err := first.Shutdown(t.Context())
	if err == nil || !strings.Contains(err.Error(), "handlers cancelled") {
		t.Fatalf("first relay's shutdown = %v, want its handler cancelled", err)
	}
	if _, pending := rows(t, db); pending != 1 {
		t.Fatalf("after the stop, %d rows pending, want 1 (event 3)", pending)
	}

	emitEach(t, db, "4")
	relay(t, db, b.Publish)
	eventually(t, "four deliveries", func() bool { return len(rec.seen()) == 4 })
	if got := rec.seen(); !slices.Equal(got, ids(1, 4)) {
		t.Fatalf("delivered %v, want %v", got, ids(1, 4))
	}
	eventually(t, "no pending rows", drained(t, db))
}

func TestFailedPublishIsRetriedInOrder(t *testing.T) {
	db := migrated(t)
	emitEach(t, db, "1", "2")
	rec := &recorder{}
	var once sync.Once
	relay(t, db, func(ctx context.Context, e event.Event) error {
		rec.add(e.ID)
		failed := false
		if e.ID == "1" {
			once.Do(func() { failed = true })
		}
		if failed {
			return errors.New("broker unavailable")
		}
		return nil
	})
	eventually(t, "three attempts", func() bool { return len(rec.seen()) == 3 })
	if got, want := rec.seen(), []string{"1", "1", "2"}; !slices.Equal(got, want) {
		t.Fatalf("attempts %v, want %v: a failed row is retried before the rows after it", got, want)
	}
	eventually(t, "no pending rows", drained(t, db))
}

// A publish that reaches the broker but whose row is never marked, as when
// the process stops between the two, is published again under the same id.
func TestPublishedButUnmarkedIsRepublished(t *testing.T) {
	db := migrated(t)
	b := memory.New()
	rec := observe(t, b)
	emitEach(t, db, "1")
	var once sync.Once
	relay(t, db, func(ctx context.Context, e event.Event) error {
		if err := b.Publish(ctx, e); err != nil {
			return err
		}
		var err error
		once.Do(func() { err = errors.New("stopped before the mark") })
		return err
	})
	eventually(t, "two deliveries", func() bool { return len(rec.seen()) == 2 })
	if got := rec.seen(); !slices.Equal(got, []string{"1", "1"}) {
		t.Fatalf("delivered %v, want event 1 twice", got)
	}
	eventually(t, "no pending rows", drained(t, db))
}

func TestRelaysPublishEachRowOnce(t *testing.T) {
	db := migrated(t)
	const n = 40
	emitEach(t, db, ids(1, n)...)
	rec := &recorder{}
	slow := func(ctx context.Context, e event.Event) error {
		time.Sleep(2 * time.Millisecond)
		return rec.record(ctx, e)
	}
	relay(t, db, slow)
	relay(t, db, slow)
	eventually(t, "no pending rows", drained(t, db))
	got := rec.seen()
	slices.SortFunc(got, func(a, b string) int {
		x, _ := strconv.Atoi(a)
		y, _ := strconv.Atoi(b)
		return x - y
	})
	if !slices.Equal(got, ids(1, n)) {
		t.Fatalf("published %d events %v, want each of 1..%d once", len(got), got, n)
	}
}

func TestShutdownSettlesTheRowInFlight(t *testing.T) {
	db := migrated(t)
	emitEach(t, db, "1")
	entered, release := make(chan struct{}), make(chan struct{})
	r := relay(t, db, func(context.Context, event.Event) error {
		close(entered)
		<-release
		return nil
	})
	<-entered
	done := make(chan error, 1)
	go func() { done <- r.Shutdown(t.Context()) }()
	time.Sleep(50 * time.Millisecond) // let the shutdown cancel the source
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if _, pending := rows(t, db); pending != 0 {
		t.Fatalf("the row in flight at shutdown is still pending")
	}
}

func TestCorruptRowEndsTheRelay(t *testing.T) {
	db := migrated(t)
	if _, err := db.ExecContext(t.Context(),
		`INSERT INTO messaging_outbox (source, id, header) VALUES ('/x', '1', '{"ce-id": ["1"]}')`); err != nil {
		t.Fatal(err)
	}
	r := relay(t, db, func(context.Context, event.Event) error { return nil })
	select {
	case err := <-r.Err():
		if err == nil || !strings.Contains(err.Error(), "corrupt row") {
			t.Fatalf("Err = %v, want a corrupt row", err)
		}
	case <-time.After(failsafe):
		t.Fatal("a corrupt row did not end the relay")
	}
}
