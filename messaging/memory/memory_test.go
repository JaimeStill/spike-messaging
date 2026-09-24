package memory_test

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
	"github.com/JaimeStill/spike-messaging/messaging/memory"
	"github.com/JaimeStill/spike-messaging/messaging/messagingtest"
)

func TestConformance(t *testing.T) {
	messagingtest.Run(t, func(*testing.T) messaging.Broker { return memory.New() })
}

// A late error, after another member has already handled the redelivery,
// must not schedule a third delivery.
func TestLateOutcomeIgnored(t *testing.T) {
	b := memory.New()
	sub := messaging.Subscription{Name: "late", AckWait: 50 * time.Millisecond}
	var calls atomic.Int32
	release := make(chan struct{})
	var wg sync.WaitGroup
	handler := func(ctx context.Context, _ event.Event) error {
		if calls.Add(1) == 1 {
			<-release // outlive AckWait, ignoring the deadline
			return errors.New("late failure")
		}
		return nil
	}
	var rs []*reactor.Reactor[event.Event]
	for range 2 {
		src, err := b.Subscribe(sub)
		if err != nil {
			t.Fatal(err)
		}
		r := reactor.New(src, handler)
		if err := r.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		rs = append(rs, r)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		deadline := time.Now().Add(2 * time.Second)
		for calls.Load() < 2 && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		close(release)
	}()
	if err := b.Publish(context.Background(), event.Event{ID: "x", Source: "/t", Type: "t"}); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	time.Sleep(200 * time.Millisecond)
	if n := calls.Load(); n != 2 {
		t.Errorf("handled %d times, want 2: the late failure must be ignored", n)
	}
	for _, r := range rs {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		if err := r.Shutdown(ctx); err != nil {
			t.Errorf("Shutdown: %v", err)
		}
		cancel()
	}
}

func TestBindingComparesMeaning(t *testing.T) {
	b := memory.New()
	types := []string{"b", "a"}
	if _, err := b.Subscribe(messaging.Subscription{Name: "n", Types: types}); err != nil {
		t.Fatal(err)
	}
	types[0] = "changed" // the consumer must keep its own copy
	if _, err := b.Subscribe(messaging.Subscription{Name: "n", Types: []string{"a", "b"}, AckWait: memory.DefaultAckWait}); err != nil {
		t.Errorf("the same types in another order, with the default AckWait spelled out, must bind: %v", err)
	}
	if _, err := b.Subscribe(messaging.Subscription{Name: "empty", Types: []string{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Subscribe(messaging.Subscription{Name: "empty"}); err != nil {
		t.Errorf("nil and empty Types mean the same filter: %v", err)
	}
}
