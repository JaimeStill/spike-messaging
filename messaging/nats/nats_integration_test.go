//go:build integration

package nats_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	libconfig "github.com/standards-lab/go-core/config"

	"github.com/JaimeStill/spike-messaging/core/event"
	"github.com/JaimeStill/spike-messaging/core/reactor"
	"github.com/JaimeStill/spike-messaging/messaging"
	"github.com/JaimeStill/spike-messaging/messaging/messagingtest"
	"github.com/JaimeStill/spike-messaging/messaging/nats"
	"github.com/JaimeStill/spike-messaging/messaging/nats/natstest"
)

const failsafe = 10 * time.Second

// broker returns a broker on a scratch stream, shut down when the test ends.
func broker(t *testing.T) *nats.Broker {
	t.Helper()
	url, stream, prefix := natstest.Scratch(t)
	return started(t, nats.Config{URL: url, Stream: stream, Prefix: prefix})
}

// started builds and starts a broker on cfg, shut down when the test ends.
func started(t *testing.T, cfg nats.Config) *nats.Broker {
	t.Helper()
	b, err := nats.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := b.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	shutdown(t, b)
	return b
}

// shutdown registers b's Shutdown as a cleanup, so it runs after the
// cleanups of the reactors started on b later in the test.
func shutdown(t *testing.T, b *nats.Broker) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), failsafe)
		defer cancel()
		if err := b.Shutdown(ctx); err != nil {
			t.Errorf("Shutdown: %v", err)
		}
	})
}

func TestConformance(t *testing.T) {
	messagingtest.Run(t, func(t *testing.T) messaging.Broker { return broker(t) })
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

func start(t *testing.T, b *nats.Broker, sub messaging.Subscription, fn reactor.Func[event.Event]) {
	t.Helper()
	src, err := b.Subscribe(sub)
	if err != nil {
		t.Fatal(err)
	}
	r := reactor.New(src, fn)
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), failsafe)
		defer cancel()
		_ = r.Shutdown(ctx)
	})
	eventually(t, "the member to be receiving", r.Ready)
}

// A late error, after another member has already handled the redelivery,
// is dropped: it neither schedules a third delivery nor, by arriving before
// the server's own AckWait, settles the redelivery.
func TestLateOutcomeDropped(t *testing.T) {
	b := broker(t)
	sub := messaging.Subscription{Name: "late", AckWait: 100 * time.Millisecond}
	var calls atomic.Int32
	release := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	handler := func(ctx context.Context, _ event.Event) error {
		if calls.Add(1) == 1 {
			defer wg.Done()
			<-release // outlive AckWait, ignoring the deadline
			return errors.New("late failure")
		}
		return nil
	}
	start(t, b, sub, handler)
	start(t, b, sub, handler)
	if err := b.Publish(t.Context(), event.Event{ID: "l", Source: "/test", Type: "t"}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the redelivery", func() bool { return calls.Load() >= 2 })
	close(release)
	wg.Wait()
	time.Sleep(3 * (sub.AckWait + nats.AckMargin))
	if n := calls.Load(); n != 2 {
		t.Errorf("handled %d times, want 2: the late outcome must be dropped", n)
	}
}

// A started broker starts once: a second Start fails and leaves the first
// connection serving.
func TestSecondStartFails(t *testing.T) {
	b := broker(t)
	if err := b.Start(t.Context()); !errors.Is(err, nats.ErrStarted) {
		t.Errorf("second Start = %v, want ErrStarted", err)
	}
	if !b.Ready() {
		t.Error("the broker is not ready after a second Start")
	}
}

func TestPublishRejectsUncarriableHeader(t *testing.T) {
	b := broker(t)
	e := event.Event{ID: "h", Source: "/test", Type: "t", Extensions: map[string]string{"note": "two\r\nlines"}}
	if err := b.Publish(t.Context(), e); err == nil {
		t.Error("Publish accepted a header value with CR LF")
	}
}

// Provisioning is idempotent: a second broker on the same stream, as a
// replica builds, converges on it and shares its events.
func TestProvisionIsIdempotent(t *testing.T) {
	url, stream, prefix := natstest.Scratch(t)
	cfg := nats.Config{URL: url, Stream: stream, Prefix: prefix}
	first := started(t, cfg)
	second := started(t, cfg)
	got := make(chan string, 1)
	start(t, second, messaging.Subscription{Name: "replica"}, func(_ context.Context, e event.Event) error {
		got <- e.ID
		return nil
	})
	if err := first.Publish(t.Context(), event.Event{ID: "shared", Source: "/test", Type: "t"}); err != nil {
		t.Fatal(err)
	}
	select {
	case id := <-got:
		if id != "shared" {
			t.Errorf("got %s", id)
		}
	case <-time.After(failsafe):
		t.Fatal("the second broker did not receive the first's event")
	}
}

// The stream's retention is the configured MaxAge, and a broker that sets
// none keeps every event.
func TestProvisionBoundsRetention(t *testing.T) {
	for _, age := range []time.Duration{0, time.Hour} {
		url, stream, prefix := natstest.Scratch(t)
		b := started(t, nats.Config{URL: url, Stream: stream, Prefix: prefix, MaxAge: libconfig.Duration(age)})
		js, err := jetstream.New(b.Conn())
		if err != nil {
			t.Fatal(err)
		}
		s, err := js.Stream(t.Context(), stream)
		if err != nil {
			t.Fatal(err)
		}
		if got := s.CachedInfo().Config.MaxAge; got != age {
			t.Errorf("stream MaxAge = %v, want %v", got, age)
		}
	}
}

// A member whose consumer is deleted while it handles a delivery, so no pull
// is waiting to hear of it, ends Receive with the error once its next pulls
// fail, rather than retrying a consumer that is gone.
func TestDeletedConsumerEndsReceive(t *testing.T) {
	url, stream, prefix := natstest.Scratch(t)
	b := started(t, nats.Config{URL: url, Stream: stream, Prefix: prefix})
	src, err := b.Subscribe(messaging.Subscription{Name: "doomed"})
	if err != nil {
		t.Fatal(err)
	}
	handling, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- src.Receive(t.Context(), func(context.Context, event.Event) error {
			close(handling)
			<-release
			return nil
		})
	}()
	eventually(t, "the source to be ready", src.Ready)
	if err := b.Publish(t.Context(), event.Event{ID: "d", Source: "/test", Type: "t"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-handling:
	case <-time.After(failsafe):
		t.Fatal("timed out waiting for the handler")
	}
	js, err := jetstream.New(b.Conn())
	if err != nil {
		t.Fatal(err)
	}
	if err := js.DeleteConsumer(t.Context(), stream, "doomed"); err != nil {
		t.Fatal(err)
	}
	close(release)
	select {
	case err := <-done:
		if err == nil {
			t.Error("Receive returned nil after its consumer was deleted")
		}
	case <-time.After(failsafe):
		t.Fatal("Receive kept retrying a deleted consumer")
	}
	if src.Ready() {
		t.Error("the source reports ready after Receive ended")
	}
}
