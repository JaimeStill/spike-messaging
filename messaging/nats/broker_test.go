package nats_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	libconfig "github.com/standards-lab/go-core/config"

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

func TestConfigValidate(t *testing.T) {
	for name, cfg := range map[string]nats.Config{
		"no url":          {Stream: "s", Prefix: "p"},
		"no stream":       {URL: "u", Prefix: "p"},
		"dotted stream":   {URL: "u", Stream: "a.b", Prefix: "p"},
		"slashed stream":  {URL: "u", Stream: "a/b", Prefix: "p"},
		"no prefix":       {URL: "u", Stream: "s"},
		"wildcard":        {URL: "u", Stream: "s", Prefix: "p.*"},
		"negative dupes":  {URL: "u", Stream: "s", Prefix: "p", Duplicates: -1},
		"negative age":    {URL: "u", Stream: "s", Prefix: "p", MaxAge: -1},
		"age below dupes": {URL: "u", Stream: "s", Prefix: "p", MaxAge: libconfig.Duration(time.Minute)},
	} {
		if cfg.Validate() == nil {
			t.Errorf("%s: Validate accepted %+v", name, cfg)
		}
		if _, err := nats.New(cfg); err == nil {
			t.Errorf("%s: New accepted %+v", name, cfg)
		}
	}
}

// Finalize fills the default URL, applies the block's environment
// overrides over the merged file values, and validates.
func TestConfigFinalize(t *testing.T) {
	cfg := nats.Config{Stream: "file", Prefix: "file"}
	cfg.Merge(&nats.Config{Stream: "overlay", MaxAge: libconfig.Duration(time.Hour)})
	t.Setenv("SVC_NATS_PREFIX", "env")
	t.Setenv("SVC_NATS_MAX_AGE", "2h")
	if err := cfg.Finalize("svc"); err != nil {
		t.Fatal(err)
	}
	want := nats.Config{URL: nats.DefaultURL, Stream: "overlay", Prefix: "env", MaxAge: libconfig.Duration(2 * time.Hour)}
	if cfg != want {
		t.Errorf("Finalize = %+v, want %+v", cfg, want)
	}

	t.Setenv("SVC_NATS_STREAM", "not.a.token")
	if err := cfg.Finalize("svc"); err == nil {
		t.Error("Finalize accepted a dotted stream from the environment")
	}
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
