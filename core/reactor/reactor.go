package reactor

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// Func handles one occurrence. Its context carries the source's values and
// is cancelled only when the drain deadline passes.
type Func[T any] func(ctx context.Context, occ T) error

// Source delivers occurrences to a Func.
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

// Reactor runs a Source into a Func. It is single use: Start once, Shutdown
// once.
type Reactor[T any] struct {
	src Source[T]
	fn  Func[T]

	mu    sync.Mutex
	state state
	stop  context.CancelFunc // ends Receive's context
	abort context.CancelFunc // cancels handler contexts
	err   error              // Receive's result once stopping
	done  chan struct{}
	errs  chan error
}

// New returns a reactor that runs src into fn.
func New[T any](src Source[T], fn Func[T]) *Reactor[T] {
	return &Reactor[T]{
		src:  src,
		fn:   fn,
		done: make(chan struct{}),
		errs: make(chan error, 1),
	}
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
		hctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
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

// Shutdown stops the source and waits for the handling in flight. If ctx
// ends first, it cancels the handlers' contexts and returns ctx's error
// without waiting further. It returns Receive's error, unless that error was
// already sent on Err.
func (r *Reactor[T]) Shutdown(ctx context.Context) error {
	r.mu.Lock()
	switch r.state {
	case idle:
		r.mu.Unlock()
		return nil
	case running:
		r.state = stopping
	}
	r.mu.Unlock()

	r.stop()
	defer r.abort()
	select {
	case <-r.done:
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.err != nil {
			return fmt.Errorf("reactor: %w", r.err)
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("reactor: drain: %w", ctx.Err())
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
