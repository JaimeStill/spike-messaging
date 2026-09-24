package scenario

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/standards-lab/go-core/lifecycle"
)

// patience bounds every wait for something a scenario expects, so a broken
// provider fails the step instead of hanging it.
const patience = 30 * time.Second

// component is what a reactor exposes to the coordinator.
type component interface {
	Start(context.Context) error
	Shutdown(context.Context) error
	Ready() bool
	Err() <-chan error
}

// coordinator runs go-core's lifecycle coordinator across a scenario's steps:
// one step starts it, later steps wait on what the reactors do, and a final
// step signals the drain. It is the one hand-written adapter from a reactor
// to a lifecycle.Service.
type coordinator struct {
	lc     *lifecycle.Coordinator
	drain  time.Duration
	cancel context.CancelFunc
	rep    *Reporter
	ready  chan struct{}
	ended  chan struct{} // closed once Run returns, after err is set
	err    error
}

func newCoordinator(drain time.Duration) *coordinator {
	return &coordinator{
		lc:    lifecycle.New(),
		drain: drain,
		ready: make(chan struct{}),
		ended: make(chan struct{}),
	}
}

// add registers r at stage and monitors its Err.
func (c *coordinator) add(name string, stage int, r component) {
	c.lc.Add(lifecycle.Service{Name: name, Stage: stage, Start: r.Start, Shutdown: r.Shutdown, Check: r})
	c.lc.Monitor(r.Err())
}

// start runs the coordinator under ctx, so an interrupt drains it, and
// returns once it is ready.
func (c *coordinator) start(ctx context.Context, rep *Reporter) error {
	c.lc.OnReady(func() { close(c.ready) })
	runCtx, cancel := context.WithCancel(ctx)
	c.cancel, c.rep = cancel, rep
	go func() {
		c.err = c.lc.Run(runCtx, c.drain)
		close(c.ended)
	}()
	if err := c.await(ctx, c.ready, "the coordinator to be ready"); err != nil {
		return err
	}
	rep.Note("ready")
	return nil
}

// await waits for ch to close. It fails if the coordinator ends first, with
// the coordinator's error when there is one, or if ctx ends or patience
// runs out.
func (c *coordinator) await(ctx context.Context, ch <-chan struct{}, what string) error {
	select {
	case <-ch:
		return nil
	case <-c.ended:
		if c.err != nil {
			return fmt.Errorf("the coordinator ended waiting for %s: %w", what, c.err)
		}
		return fmt.Errorf("the coordinator ended waiting for %s", what)
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(patience):
		return fmt.Errorf("timed out waiting for %s", what)
	}
}

// stop signals the drain and returns Run's result: nil for a clean drain,
// which it notes.
func (c *coordinator) stop(rep *Reporter) error {
	if c.cancel == nil {
		return errors.New("the coordinator never started")
	}
	c.cancel()
	<-c.ended
	if c.err == nil {
		rep.Note("drained cleanly")
	}
	return c.err
}

// cleanup drains a coordinator a step left running, as when an interrupt or
// a failed step ends the scenario early, and narrates the drain. A
// coordinator that had already ended was reported by the step that saw it,
// so cleanup reports only a drain it performed itself.
func (c *coordinator) cleanup() error {
	if c.cancel == nil {
		return nil
	}
	select {
	case <-c.ended:
		return nil
	default:
	}
	c.rep.Note("draining the coordinator")
	return c.stop(c.rep)
}
