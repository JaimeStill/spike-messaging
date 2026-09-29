package lifecycle_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	golifecycle "github.com/standards-lab/go-core/lifecycle"

	"github.com/JaimeStill/spike-messaging/core/lifecycle"
)

// failsafe bounds every wait for something that should happen, so a broken
// registration fails the test instead of hanging it.
const failsafe = 2 * time.Second

// fake is a component that records its lifecycle calls.
type fake struct {
	started, stopped atomic.Bool
	err              chan error
}

func (f *fake) Start(context.Context) error    { f.started.Store(true); return nil }
func (f *fake) Shutdown(context.Context) error { f.stopped.Store(true); return nil }
func (f *fake) Ready() bool                    { return f.started.Load() }
func (f *fake) Err() <-chan error              { return f.err }

// A registered component starts with the run, is the named readiness check,
// and a failure it sends on Err ends the run and drains it.
func TestRegister(t *testing.T) {
	lc := golifecycle.New()
	f := &fake{err: make(chan error, 1)}
	lifecycle.Register(lc, "fake", 0, f)

	checks := lc.Checks()
	if len(checks) != 1 || checks[0].Name != "fake" {
		t.Fatalf("Checks = %+v, want the one check named fake", checks)
	}

	ready := make(chan struct{})
	lc.OnReady(func() { close(ready) })
	done := make(chan error, 1)
	go func() { done <- lc.Run(context.Background(), failsafe) }()

	select {
	case <-ready:
	case <-time.After(failsafe):
		t.Fatal("timed out waiting for the run to be ready")
	}
	if !f.started.Load() || !checks[0].Checker.Ready() {
		t.Fatal("the component did not start, or its check is not ready")
	}

	boom := errors.New("boom")
	f.err <- boom
	select {
	case err := <-done:
		if !errors.Is(err, boom) {
			t.Errorf("Run = %v, want the component's failure", err)
		}
	case <-time.After(failsafe):
		t.Fatal("a failure on Err did not end the run")
	}
	if !f.stopped.Load() {
		t.Error("the run ended without draining the component")
	}
}
