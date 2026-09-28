package app_test

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JaimeStill/spike-messaging/services/exercise/internal/app"
	"github.com/JaimeStill/spike-messaging/services/exercise/internal/config/configtest"
)

// failsafe bounds every wait for an event that should occur, so a broken
// composition fails the test instead of hanging it.
const failsafe = 2 * time.Second

// syncBuffer serializes writes so the app's logging goroutines and the
// test's reads stay race-free.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// The composition's startup contract: with the database and NATS on closed
// ports, Run fails its startup, exits 1, names the failed subsystem, and
// the server never reports ready. The serve-and-drain pass against real
// backing services is the integration tier's.
func TestRun_FailsStartupWithoutBackingServices(t *testing.T) {
	buf := &syncBuffer{}
	a, err := app.New(configtest.Config(t), buf)
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}

	done := make(chan int, 1)
	go func() { done <- a.Run(context.Background()) }()

	select {
	case code := <-done:
		if code != 1 {
			t.Errorf("Run = %d, want 1 on a failed startup", code)
		}
	case <-time.After(failsafe):
		t.Fatal("timed out waiting for Run to fail startup")
	}

	out := buf.String()
	if !strings.Contains(out, "database") && !strings.Contains(out, "broker") {
		t.Errorf("failure log names neither the database nor the broker: %q", out)
	}
	if strings.Contains(out, "server ready") {
		t.Error("log carries a ready record despite the failed startup")
	}
}

// A second Run cannot exist: the coordinator is single-use, and the exit
// path reports rather than panics only for lifecycle errors — a re-run is a
// programming error and propagates go-core's panic.
func TestRun_TwicePanics(t *testing.T) {
	buf := &syncBuffer{}
	a, err := app.New(configtest.Config(t), buf)
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = a.Run(ctx)

	defer func() {
		if recover() == nil {
			t.Error("a second Run did not panic")
		}
	}()
	_ = a.Run(ctx)
}
