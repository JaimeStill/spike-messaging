package nats_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/JaimeStill/spike-messaging/core/event"
	"github.com/JaimeStill/spike-messaging/messaging"
	"github.com/JaimeStill/spike-messaging/messaging/nats"
)

// closedURL is a NATS URL on a loopback port nothing listens on.
func closedURL(t *testing.T) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := lis.Addr().(*net.TCPAddr).Port
	if err := lis.Close(); err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("nats://127.0.0.1:%d", port)
}

// Before Start, the broker is not ready, and a publish or a receive fails
// rather than reach a connection that does not exist; the coordinator's
// stage order keeps both from happening in a process.
func TestBrokerBeforeStart(t *testing.T) {
	b, err := nats.New(nats.Config{URL: closedURL(t), Stream: "test", Prefix: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if b.Ready() || b.Conn() != nil {
		t.Error("an unstarted broker is ready, or has a connection")
	}
	if err := b.Publish(t.Context(), event.Event{ID: "1", Source: "/test", Type: "t"}); !errors.Is(err, nats.ErrNotStarted) {
		t.Errorf("Publish = %v, want ErrNotStarted", err)
	}
	if err := b.Shutdown(t.Context()); err != nil {
		t.Errorf("Shutdown of an unstarted broker = %v", err)
	}

	if _, err := b.Subscribe(messaging.Subscription{Name: "not.a.token"}); err == nil {
		t.Error("Subscribe accepted a dotted name")
	}
	src, err := b.Subscribe(messaging.Subscription{Name: "orders"})
	if err != nil {
		t.Fatal(err)
	}
	if src.Ready() {
		t.Error("a subscription is ready before it receives")
	}
	err = src.Receive(t.Context(), func(context.Context, event.Event) error { return nil })
	if !errors.Is(err, nats.ErrNotStarted) {
		t.Errorf("Receive = %v, want ErrNotStarted", err)
	}
}

// A broker whose server is unreachable fails its Start, which fails the
// process's startup, and stays not ready.
func TestBrokerStartFailsWithoutNATS(t *testing.T) {
	b, err := nats.New(nats.Config{URL: closedURL(t), Stream: "test", Prefix: "test"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := b.Start(ctx); err == nil {
		t.Fatal("Start succeeded against a closed port")
	}
	if b.Ready() {
		t.Error("a broker that failed to start is ready")
	}
	// A failed Start leaves the broker free to start again.
	if err := b.Start(ctx); errors.Is(err, nats.ErrStarted) {
		t.Error("a Start after a failed one reports the broker started")
	}
}
