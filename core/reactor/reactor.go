package reactor

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Func handles one occurrence. Its context carries the source's values and
// any deadline the source set for the occurrence, and is otherwise cancelled
// only when the drain cancels the handlers.
type Func[T any] func(ctx context.Context, occ T) error

// Source delivers occurrences to a Func.
//
// What a handler error means is the source's own rule. An interval ([Every])
// has nothing to redeliver, so an error ends it; a subscription redelivers
// the occurrence instead and keeps receiving. A source may bound each
// occurrence with a deadline on the context it passes to fn, such as a
// broker's acknowledgement wait; the reactor keeps that deadline while
// detaching the handler from the source's own cancellation.
type Source[T any] interface {
	// Receive delivers each occurrence to fn until ctx ends, then waits for
	// the handling in flight and returns nil. It returns an error when the
	// source fails, or when fn's error ends it.
	Receive(ctx context.Context, fn Func[T]) error
	// Ready reports whether the source is receiving.
	Ready() bool
}

type state int

const (
	idle state = iota
	running
	stopping
	stopped
)

// Option configures a Reactor.
type Option func(*options)

type options struct {
	grace time.Duration
}

// Grace bounds how long Shutdown lets the handling in flight run before it
// cancels the handlers' contexts. Shutdown then waits for them to unwind,
// until its own context ends. Set it below the coordinator's drain timeout,
// so a reactor that had to cancel its handlers says so in Run's result; the
// coordinator drops any error that arrives after its own deadline. Without
// Grace, the handlers are cancelled only when Shutdown's context ends.
func Grace(d time.Duration) Option {
	return func(o *options) { o.grace = d }
}

// Reactor runs a Source into a Func. It is single use: Start once, Shutdown
// once.
type Reactor[T any] struct {
	src  Source[T]
	fn   Func[T]
	opts options

	mu    sync.Mutex
	state state
	stop  context.CancelFunc // ends Receive's context
	abort context.CancelFunc // cancels handler contexts
	err   error              // Receive's result once stopping
	done  chan struct{}
	errs  chan error
}

// New returns a reactor that runs src into fn.
func New[T any](src Source[T], fn Func[T], opts ...Option) *Reactor[T] {
	r := &Reactor[T]{
		src:  src,
		fn:   fn,
		done: make(chan struct{}),
		errs: make(chan error, 1),
	}
	for _, opt := range opts {
		opt(&r.opts)
	}
	return r
}

// Start launches the source and returns. The reactor keeps ctx's values but
// not its cancellation; only Shutdown stops it.
func (r *Reactor[T]) Start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state != idle {
		return errors.New("reactor: started twice")
	}
	base := context.WithoutCancel(ctx)
	recvCtx, stop := context.WithCancel(base)
	abortCtx, abort := context.WithCancel(base)
	r.stop, r.abort = stop, abort
	r.state = running

	handle := func(ctx context.Context, occ T) error {
		// Stopping the source must not interrupt the handler, but a deadline
		// the source set for this occurrence still applies.
		var hctx context.Context
		var cancel context.CancelFunc
		if d, ok := ctx.Deadline(); ok {
			hctx, cancel = context.WithDeadline(context.WithoutCancel(ctx), d)
		} else {
			hctx, cancel = context.WithCancel(context.WithoutCancel(ctx))
		}
		defer cancel()
		defer context.AfterFunc(abortCtx, cancel)()
		return r.fn(hctx, occ)
	}

	go func() {
		err := r.src.Receive(recvCtx, handle)
		r.mu.Lock()
		if r.state == stopping {
			r.err = err
		} else if err != nil {
			r.errs <- fmt.Errorf("reactor: %w", err)
		}
		r.state = stopped
		r.mu.Unlock()
		close(r.errs)
		close(r.done)
	}()
	return nil
}

// Shutdown drains the reactor in two phases. It stops the source and waits
// for the handling in flight; once the [Grace] period passes, it cancels the
// handlers' contexts and waits for them to unwind. It returns once Receive
// returns, or when ctx ends, which cancels the handlers and stops waiting.
// The error reports a failure sent on Err that nothing read, a grace that
// ran out, or ctx's error, or else Receive's error. Shutdown before Start
// retires the reactor, so it never starts.
func (r *Reactor[T]) Shutdown(ctx context.Context) error {
	r.mu.Lock()
	switch r.state {
	case idle:
		r.state = stopped
		close(r.errs)
		close(r.done)
		r.mu.Unlock()
		return nil
	case running:
		r.state = stopping
	}
	r.mu.Unlock()

	r.stop()
	defer r.abort()

	var grace <-chan time.Time
	if r.opts.grace > 0 {
		t := time.NewTimer(r.opts.grace)
		defer t.Stop()
		grace = t.C
	}
	cancelled := false
	for {
		select {
		case <-grace:
			grace = nil
			select {
			case <-r.done:
				continue // the drain finished as the grace ran out
			default:
			}
			r.abort()
			cancelled = true
		case <-ctx.Done():
			return fmt.Errorf("reactor: drain: %w", ctx.Err())
		case <-r.done:
			// An error sent on Err after the coordinator stopped monitoring
			// has no other reader; report it here rather than lose it.
			if err, ok := <-r.errs; ok {
				return err
			}
			r.mu.Lock()
			err := r.err
			r.mu.Unlock()
			if cancelled {
				// A handler that unwinds returns its cancellation; that is
				// the expected outcome, not a second failure.
				if errors.Is(err, context.Canceled) {
					err = nil
				}
				return errors.Join(
					fmt.Errorf("reactor: handlers cancelled after grace %v", r.opts.grace),
					err,
				)
			}
			if err != nil {
				return fmt.Errorf("reactor: %w", err)
			}
			return nil
		}
	}
}

// Ready reports whether the reactor is running and its source is ready. It
// turns false once Shutdown begins.
func (r *Reactor[T]) Ready() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state == running && r.src.Ready()
}

// Err yields the error that ends the source while the reactor is running,
// then closes when the source returns. A composition root passes it to the
// coordinator's Monitor, so a dead reactor ends the process.
func (r *Reactor[T]) Err() <-chan error {
	return r.errs
}
