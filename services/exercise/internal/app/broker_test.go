package app

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

var testConfig = nats.Config{Stream: "test", Prefix: "test"}

func TestNewBrokerChecksItsConfig(t *testing.T) {
	if _, err := newBroker("nats://127.0.0.1:4222", "exercise", nats.Config{Stream: "a.b", Prefix: "p"}); err == nil {
		t.Error("newBroker accepted a dotted stream name")
	}
}

// Before Start, the broker is not ready, and a publish or a receive fails
// rather than reach a broker that does not exist; the coordinator's stage
// order keeps both from happening in the process.
func TestBrokerBeforeStart(t *testing.T) {
	b, err := newBroker(closedURL(t), "exercise", testConfig)
	if err != nil {
		t.Fatal(err)
	}
	if b.Ready() {
		t.Error("an unstarted broker is ready")
	}
	if err := b.Publish(t.Context(), event.Event{ID: "1", Source: "/test", Type: "t"}); !errors.Is(err, errNotStarted) {
		t.Errorf("Publish = %v, want errNotStarted", err)
	}
	if err := b.Shutdown(t.Context()); err != nil {
		t.Errorf("Shutdown of an unstarted broker = %v", err)
	}

	src, err := b.Subscribe(messaging.Subscription{Name: "orders"})
	if err != nil {
		t.Fatal(err)
	}
	if src.Ready() {
		t.Error("a subscription is ready before it receives")
	}
	err = src.Receive(t.Context(), func(context.Context, event.Event) error { return nil })
	if !errors.Is(err, errNotStarted) {
		t.Errorf("Receive = %v, want errNotStarted", err)
	}
}

func TestBrokerSubscribeChecksTheSubscription(t *testing.T) {
	b, err := newBroker(closedURL(t), "exercise", testConfig)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Subscribe(messaging.Subscription{Name: "not.a.token"}); err == nil {
		t.Error("Subscribe accepted a dotted name")
	}
}

// A broker whose NATS is unreachable fails its Start, which fails the
// process's startup, and stays not ready.
func TestBrokerStartFailsWithoutNATS(t *testing.T) {
	b, err := newBroker(closedURL(t), "exercise", testConfig)
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
}
