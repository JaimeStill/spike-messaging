// Package messagingtest is the conformance suite every messaging provider
// passes. A provider's test calls [Run] with a constructor for a fresh
// broker; each case runs its sources through a core/reactor Reactor, as a
// composition root would.
//
// The cases prove what a service relies on: an event survives the broker
// intact, the type filter and delivery groups route it, the handler's return
// decides its outcome, AckWait bounds a handler and redelivers, a durable
// keeps its position across members, and a drained handler's
// acknowledgement holds. A case that proves an absence, such as no
// redelivery after a terminate, watches a short quiet window.
package messagingtest

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/JaimeStill/spike-messaging/core/event"
	"github.com/JaimeStill/spike-messaging/core/reactor"
	"github.com/JaimeStill/spike-messaging/messaging"
)

// failsafe bounds every wait for something that should happen, so a broken
// provider fails the case instead of hanging it.
const failsafe = 5 * time.Second

// quiet is how long a case watches for something that must not happen.
const quiet = 250 * time.Millisecond

// Run runs the conformance suite, each case as a subtest on a broker from
// newBroker.
func Run(t *testing.T, newBroker func(t *testing.T) messaging.Broker) {
	cases := []struct {
		name string
		fn   func(*testing.T, messaging.Broker)
	}{
		{"RoundTrip", testRoundTrip},
		{"TypeFilter", testTypeFilter},
		{"NamesEachReceiveAll", testNamesEachReceiveAll},
		{"GroupSplitsWork", testGroupSplitsWork},
		{"ErrorRedelivers", testErrorRedelivers},
		{"PermanentTerminates", testPermanentTerminates},
		{"MaxDeliverBounds", testMaxDeliverBounds},
		{"AckWaitRedelivers", testAckWaitRedelivers},
		{"DurableResumes", testDurableResumes},
		{"DrainKeepsAck", testDrainKeepsAck},
		{"Ready", testReady},
		{"SubscribeRejectsInvalid", testSubscribeRejectsInvalid},
		{"PublishRejectsInvalid", testPublishRejectsInvalid},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { c.fn(t, newBroker(t)) })
	}
}

func ev(id, typ string) event.Event {
	return event.Event{ID: id, Source: "/messagingtest", Type: typ}
}

func publish(t *testing.T, b messaging.Broker, es ...event.Event) {
	t.Helper()
	for _, e := range es {
		if err := b.Publish(context.Background(), e); err != nil {
			t.Fatalf("Publish %s: %v", e.ID, err)
		}
	}
}

// member subscribes under sub, starts a reactor on the source, and waits
// until it is receiving. The cleanup shuts it down if the case has not.
func member(t *testing.T, b messaging.Broker, sub messaging.Subscription, fn reactor.Func[event.Event]) *reactor.Reactor[event.Event] {
	t.Helper()
	src, err := b.Subscribe(sub)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	r := reactor.New(src, fn)
	if err := r.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = stop(r) })
	eventually(t, r.Ready, "the member to be receiving")
	return r
}

func stop(r *reactor.Reactor[event.Event]) error {
	ctx, cancel := context.WithTimeout(context.Background(), failsafe)
	defer cancel()
	return r.Shutdown(ctx)
}

func eventually(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(failsafe)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// deliveries records what each handler received.
type deliveries struct {
	mu  sync.Mutex
	ids []string
	by  map[string]string // id to the member that handled it last
}

func (d *deliveries) add(who string, e event.Event) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.ids = append(d.ids, e.ID)
	if d.by == nil {
		d.by = map[string]string{}
	}
	d.by[e.ID] = who
}

func (d *deliveries) seen() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.ids...)
}

func (d *deliveries) count(id string) int {
	n := 0
	for _, got := range d.seen() {
		if got == id {
			n++
		}
	}
	return n
}

func (d *deliveries) has(id string) func() bool {
	return func() bool { return d.count(id) > 0 }
}

// record returns a handler that records each event and acknowledges it.
func (d *deliveries) record(who string) reactor.Func[event.Event] {
	return func(_ context.Context, e event.Event) error {
		d.add(who, e)
		return nil
	}
}

func testRoundTrip(t *testing.T, b messaging.Broker) {
	want := event.Event{
		ID:              "rt-1",
		Source:          "/grants",
		Type:            "lab.grant.approved",
		Subject:         "grant/42",
		DataContentType: "application/json",
		DataSchema:      "https://example.com/schemas/grant.json",
		Time:            time.Date(2026, 9, 24, 13, 0, 0, 123456789, time.UTC),
		Data:            []byte(`{"id":42}`),
		Extensions:      map[string]string{"traceparent": "00-abc-def-01"},
	}
	got := make(chan event.Event, 1)
	member(t, b, messaging.Subscription{Name: "rt"}, func(_ context.Context, e event.Event) error {
		got <- e
		return nil
	})
	publish(t, b, want)
	select {
	case e := <-got:
		if !reflect.DeepEqual(e, want) {
			t.Errorf("round trip:\n got %+v\nwant %+v", e, want)
		}
	case <-time.After(failsafe):
		t.Fatal("timed out waiting for the event")
	}
}

func testTypeFilter(t *testing.T, b messaging.Broker) {
	var d deliveries
	member(t, b, messaging.Subscription{Name: "filtered", Types: []string{"want"}}, d.record("m"))
	publish(t, b, ev("a", "skip"), ev("b", "want"), ev("c", "skip"), ev("d", "want"), ev("end", "want"))
	eventually(t, d.has("end"), "the last matching event")
	if got, want := d.seen(), []string{"b", "d", "end"}; !reflect.DeepEqual(got, want) {
		t.Errorf("delivered %v, want %v", got, want)
	}
}

func testNamesEachReceiveAll(t *testing.T, b messaging.Broker) {
	var x, y deliveries
	member(t, b, messaging.Subscription{Name: "x"}, x.record("x"))
	member(t, b, messaging.Subscription{Name: "y"}, y.record("y"))
	want := []string{"1", "2", "3", "4", "5"}
	for _, id := range want {
		publish(t, b, ev(id, "t"))
	}
	eventually(t, func() bool { return len(x.seen()) == len(want) && len(y.seen()) == len(want) }, "both names to receive every event")
	if !reflect.DeepEqual(x.seen(), want) || !reflect.DeepEqual(y.seen(), want) {
		t.Errorf("x got %v, y got %v, want each %v", x.seen(), y.seen(), want)
	}
}

func testGroupSplitsWork(t *testing.T, b messaging.Broker) {
	var d deliveries
	slow := func(who string) reactor.Func[event.Event] {
		return func(_ context.Context, e event.Event) error {
			time.Sleep(10 * time.Millisecond)
			d.add(who, e)
			return nil
		}
	}
	sub := messaging.Subscription{Name: "workers"}
	member(t, b, sub, slow("a"))
	member(t, b, sub, slow("b"))
	const n = 20
	for i := range n {
		publish(t, b, ev(fmt.Sprint(i), "t"))
	}
	eventually(t, func() bool { return len(d.seen()) >= n }, "the group to handle every event")
	perMember := map[string]int{}
	for i := range n {
		id := fmt.Sprint(i)
		if c := d.count(id); c != 1 {
			t.Errorf("event %s handled %d times, want once", id, c)
		}
		d.mu.Lock()
		perMember[d.by[id]]++
		d.mu.Unlock()
	}
	if perMember["a"] == 0 || perMember["b"] == 0 {
		t.Errorf("work per member = %v, want both members to share it", perMember)
	}
}

func testErrorRedelivers(t *testing.T, b messaging.Broker) {
	const delay = 100 * time.Millisecond
	var mu sync.Mutex
	var times []time.Time
	member(t, b, messaging.Subscription{Name: "retry", RetryDelay: delay}, func(_ context.Context, _ event.Event) error {
		mu.Lock()
		defer mu.Unlock()
		times = append(times, time.Now())
		if len(times) == 1 {
			return errors.New("transient")
		}
		return nil
	})
	publish(t, b, ev("r", "t"))
	eventually(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(times) >= 2 }, "the redelivery")
	mu.Lock()
	gap := times[1].Sub(times[0])
	mu.Unlock()
	if gap < delay {
		t.Errorf("redelivered after %v, want no sooner than %v", gap, delay)
	}
	time.Sleep(quiet)
	mu.Lock()
	defer mu.Unlock()
	if len(times) != 2 {
		t.Errorf("handled %d times, want 2: an acknowledged redelivery is final", len(times))
	}
}

func testPermanentTerminates(t *testing.T, b messaging.Broker) {
	var d deliveries
	member(t, b, messaging.Subscription{Name: "perm", RetryDelay: 10 * time.Millisecond}, func(ctx context.Context, e event.Event) error {
		d.add("m", e)
		if e.ID == "p" {
			return fmt.Errorf("handle: %w", event.Permanent(errors.New("malformed")))
		}
		return nil
	})
	publish(t, b, ev("p", "t"), ev("end", "t"))
	eventually(t, d.has("end"), "the event after the terminated one")
	time.Sleep(quiet)
	if c := d.count("p"); c != 1 {
		t.Errorf("the terminated event was handled %d times, want once", c)
	}
}

func testMaxDeliverBounds(t *testing.T, b messaging.Broker) {
	var d deliveries
	member(t, b, messaging.Subscription{Name: "bounded", MaxDeliver: 3, RetryDelay: 10 * time.Millisecond}, func(_ context.Context, e event.Event) error {
		d.add("m", e)
		return errors.New("always")
	})
	publish(t, b, ev("m", "t"))
	eventually(t, func() bool { return d.count("m") >= 3 }, "three deliveries")
	time.Sleep(quiet)
	if c := d.count("m"); c != 3 {
		t.Errorf("delivered %d times, want MaxDeliver 3", c)
	}
}

func testAckWaitRedelivers(t *testing.T, b messaging.Broker) {
	const wait = 100 * time.Millisecond
	type attempt struct {
		hasDeadline bool
		err         error
	}
	var mu sync.Mutex
	var attempts []attempt
	member(t, b, messaging.Subscription{Name: "slow", AckWait: wait, RetryDelay: 10 * time.Millisecond}, func(ctx context.Context, _ event.Event) error {
		mu.Lock()
		n := len(attempts)
		attempts = append(attempts, attempt{})
		mu.Unlock()
		_, ok := ctx.Deadline()
		var err error
		if n == 0 {
			select {
			case <-ctx.Done():
				err = ctx.Err()
			case <-time.After(failsafe):
			}
		}
		mu.Lock()
		attempts[n] = attempt{ok, err}
		mu.Unlock()
		return nil // late on the first attempt, so ignored
	})
	publish(t, b, ev("s", "t"))
	eventually(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(attempts) >= 2 }, "the redelivery after AckWait")
	mu.Lock()
	first := attempts[0]
	mu.Unlock()
	if !first.hasDeadline {
		t.Error("the handler's context carries no AckWait deadline")
	}
	if !errors.Is(first.err, context.DeadlineExceeded) {
		t.Errorf("the first attempt's context ended with %v, want the AckWait deadline", first.err)
	}
	time.Sleep(quiet)
	mu.Lock()
	defer mu.Unlock()
	if len(attempts) != 2 {
		t.Errorf("handled %d times, want 2", len(attempts))
	}
}

func testDurableResumes(t *testing.T, b messaging.Broker) {
	sub := messaging.Subscription{Name: "durable"}
	var first deliveries
	m1 := member(t, b, sub, first.record("1"))
	publish(t, b, ev("e1", "t"))
	eventually(t, first.has("e1"), "the first member to handle e1")
	if err := stop(m1); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	publish(t, b, ev("e2", "t"), ev("e3", "t"))
	var second deliveries
	member(t, b, sub, second.record("2"))
	eventually(t, second.has("e3"), "the second member to catch up")
	if got, want := second.seen(), []string{"e2", "e3"}; !reflect.DeepEqual(got, want) {
		t.Errorf("the resumed member got %v, want %v", got, want)
	}
}

func testDrainKeepsAck(t *testing.T, b messaging.Broker) {
	sub := messaging.Subscription{Name: "drain", RetryDelay: 10 * time.Millisecond}
	started := make(chan struct{})
	var finished bool
	m1 := member(t, b, sub, func(ctx context.Context, _ event.Event) error {
		close(started)
		time.Sleep(100 * time.Millisecond)
		finished = ctx.Err() == nil
		return nil
	})
	publish(t, b, ev("in-flight", "t"))
	select {
	case <-started:
	case <-time.After(failsafe):
		t.Fatal("timed out waiting for the handler")
	}
	if err := stop(m1); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if !finished {
		t.Fatal("the drain interrupted the handler")
	}
	var d deliveries
	member(t, b, sub, d.record("2"))
	publish(t, b, ev("end", "t"))
	eventually(t, d.has("end"), "the next member to receive new events")
	time.Sleep(quiet)
	if c := d.count("in-flight"); c != 0 {
		t.Errorf("the drained event was redelivered %d times; its acknowledgement was lost", c)
	}
}

func testReady(t *testing.T, b messaging.Broker) {
	src, err := b.Subscribe(messaging.Subscription{Name: "ready"})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if src.Ready() {
		t.Error("ready before receiving")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- src.Receive(ctx, func(context.Context, event.Event) error { return nil })
	}()
	eventually(t, src.Ready, "the source to be ready")
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Receive: %v", err)
		}
	case <-time.After(failsafe):
		t.Fatal("Receive did not return after its context ended")
	}
	if src.Ready() {
		t.Error("ready after Receive returned")
	}
}

func testSubscribeRejectsInvalid(t *testing.T, b messaging.Broker) {
	if _, err := b.Subscribe(messaging.Subscription{}); err == nil {
		t.Error("Subscribe accepted a subscription with no name")
	}
}

func testPublishRejectsInvalid(t *testing.T, b messaging.Broker) {
	if err := b.Publish(context.Background(), event.Event{ID: "no-source"}); err == nil {
		t.Error("Publish accepted an invalid event")
	}
}
