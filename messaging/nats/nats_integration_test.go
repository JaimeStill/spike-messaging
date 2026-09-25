//go:build integration

package nats_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JaimeStill/spike-messaging/core/event"
	"github.com/JaimeStill/spike-messaging/core/reactor"
	"github.com/JaimeStill/spike-messaging/messaging"
	"github.com/JaimeStill/spike-messaging/messaging/messagingtest"
	"github.com/JaimeStill/spike-messaging/messaging/nats"
	"github.com/JaimeStill/spike-messaging/messaging/nats/internal/natstest"
)

const failsafe = 10 * time.Second

// broker returns a broker on a scratch stream, shut down when the test ends.
func broker(t *testing.T) *nats.Broker {
	t.Helper()
	nc, stream, prefix := natstest.Scratch(t)
	b, err := nats.New(t.Context(), nc, nats.Config{Stream: stream, Prefix: prefix})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), failsafe)
		defer cancel()
		if err := b.Shutdown(ctx); err != nil {
			t.Errorf("Shutdown: %v", err)
		}
	})
	return b
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
	nc, stream, prefix := natstest.Scratch(t)
	cfg := nats.Config{Stream: stream, Prefix: prefix}
	first, err := nats.New(t.Context(), nc, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Shutdown(context.Background()) }()
	nc2, _, _ := natstest.Scratch(t)
	second, err := nats.New(t.Context(), nc2, cfg)
	if err != nil {
		t.Fatalf("a second New on the same stream: %v", err)
	}
	defer func() { _ = second.Shutdown(context.Background()) }()
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

func TestConfigValidate(t *testing.T) {
	for name, cfg := range map[string]nats.Config{
		"no stream":      {Prefix: "p"},
		"dotted stream":  {Stream: "a.b", Prefix: "p"},
		"no prefix":      {Stream: "s"},
		"wildcard":       {Stream: "s", Prefix: "p.*"},
		"negative dupes": {Stream: "s", Prefix: "p", Duplicates: -1},
	} {
		if cfg.Validate() == nil {
			t.Errorf("%s: Validate accepted %+v", name, cfg)
		}
	}
}
