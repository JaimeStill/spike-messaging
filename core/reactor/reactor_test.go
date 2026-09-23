package reactor_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JaimeStill/spike-messaging/core/reactor"
)

// failsafe bounds every wait for something that should happen, so a broken
// reactor fails the test instead of hanging it.
const failsafe = 2 * time.Second

func recvOrFail[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(failsafe):
		t.Fatalf("timed out waiting for %s", what)
		var zero T
		return zero
	}
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

func shutdown(t *testing.T, r interface{ Shutdown(context.Context) error }, d time.Duration) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	return r.Shutdown(ctx)
}

func TestShutdownDrainsInFlight(t *testing.T) {
	started := make(chan struct{}, 1)
	var calls atomic.Int32
	var ended atomic.Bool
	r := reactor.New(reactor.Every(5*time.Millisecond), func(ctx context.Context, _ time.Time) error {
		calls.Add(1)
		select {
		case started <- struct{}{}:
		default:
		}
		time.Sleep(50 * time.Millisecond)
		ended.Store(ctx.Err() != nil)
		return nil
	})

	runCtx, cancelRun := context.WithCancel(context.Background())
	if err := r.Start(runCtx); err != nil {
		t.Fatal(err)
	}
	recvOrFail(t, started, "the first tick")
	cancelRun() // the coordinator cancels the run context before draining

	if err := shutdown(t, r, failsafe); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if ended.Load() {
		t.Error("the handler's context ended before the drain finished")
	}
	n := calls.Load()
	time.Sleep(30 * time.Millisecond)
	if calls.Load() != n {
		t.Error("a tick was handled after Shutdown returned")
	}
}

func TestRunContextDoesNotStop(t *testing.T) {
	var calls atomic.Int32
	r := reactor.New(reactor.Every(2*time.Millisecond), func(context.Context, time.Time) error {
		calls.Add(1)
		return nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	if err := r.Start(ctx); err != nil {
		t.Fatal(err)
	}
	cancel()
	eventually(t, func() bool { return calls.Load() >= 3 }, "ticks after the run context ended")
	if err := shutdown(t, r, failsafe); err != nil {
		t.Fatal(err)
	}
}

func TestShutdownDeadlineCancelsHandler(t *testing.T) {
	started := make(chan struct{}, 1)
	cancelled := make(chan error, 1)
	r := reactor.New(reactor.Every(time.Millisecond), func(ctx context.Context, _ time.Time) error {
		started <- struct{}{}
		<-ctx.Done()
		cancelled <- ctx.Err()
		return nil
	})
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	recvOrFail(t, started, "the handler")
	err := shutdown(t, r, 20*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown = %v, want DeadlineExceeded", err)
	}
	if got := recvOrFail(t, cancelled, "the handler's cancellation"); !errors.Is(got, context.Canceled) {
		t.Errorf("handler context error = %v", got)
	}
}

func TestHandlerErrorOnErr(t *testing.T) {
	boom := errors.New("boom")
	r := reactor.New(reactor.Every(time.Millisecond), func(context.Context, time.Time) error {
		return boom
	})
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := recvOrFail(t, r.Err(), "the failure"); !errors.Is(err, boom) {
		t.Fatalf("Err yielded %v", err)
	}
	if _, open := <-r.Err(); open {
		t.Error("Err did not close after the source ended")
	}
	if r.Ready() {
		t.Error("a failed reactor reports ready")
	}
	if err := shutdown(t, r, failsafe); err != nil {
		t.Errorf("Shutdown after a reported failure = %v, want nil", err)
	}
}

func TestErrorWhileDrainingGoesToShutdown(t *testing.T) {
	boom := errors.New("boom")
	src := &stub{err: boom}
	r := reactor.New[int](src, func(context.Context, int) error { return nil })
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	eventually(t, r.Ready, "readiness")
	if err := shutdown(t, r, failsafe); !errors.Is(err, boom) {
		t.Fatalf("Shutdown = %v, want boom", err)
	}
	if _, open := <-r.Err(); open {
		t.Error("Err yielded an error sent to Shutdown")
	}
}

func TestReady(t *testing.T) {
	r := reactor.New(reactor.Every(time.Hour), func(context.Context, time.Time) error { return nil })
	if r.Ready() {
		t.Error("ready before Start")
	}
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	eventually(t, r.Ready, "readiness")
	if err := shutdown(t, r, failsafe); err != nil {
		t.Fatal(err)
	}
	if r.Ready() {
		t.Error("ready after Shutdown")
	}
}

func TestStartTwice(t *testing.T) {
	r := reactor.New(reactor.Every(time.Hour), func(context.Context, time.Time) error { return nil })
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := r.Start(context.Background()); err == nil {
		t.Error("second Start succeeded")
	}
	if err := shutdown(t, r, failsafe); err != nil {
		t.Fatal(err)
	}
}

func TestShutdownBeforeStart(t *testing.T) {
	r := reactor.New(reactor.Every(time.Hour), func(context.Context, time.Time) error { return nil })
	if err := shutdown(t, r, failsafe); err != nil {
		t.Errorf("Shutdown before Start = %v", err)
	}
}

func TestEveryRejectsNonPositive(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("Every(0) did not panic")
		}
	}()
	reactor.Every(0)
}

// stub is a source that is ready until its context ends, then returns err.
type stub struct {
	err   error
	ready atomic.Bool
}

func (s *stub) Receive(ctx context.Context, _ reactor.Func[int]) error {
	s.ready.Store(true)
	<-ctx.Done()
	s.ready.Store(false)
	return s.err
}

func (s *stub) Ready() bool { return s.ready.Load() }
