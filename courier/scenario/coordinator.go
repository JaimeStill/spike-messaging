package scenario

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/standards-lab/go-core/lifecycle"

	corelifecycle "github.com/JaimeStill/spike-messaging/core/lifecycle"
)

// patience bounds every wait for something a scenario expects, so a broken
// provider fails the step instead of hanging it.
const patience = 30 * time.Second

// coordinator runs go-core's lifecycle coordinator across a scenario's steps:
// one step starts it, later steps wait on what the reactors do, and a final
// step signals the drain. A step registers each reactor on lc through
// core/lifecycle.Register.
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

// await waits for ch to close. It fails if the coordinator ends first,
// wrapping its error when there is one, if ctx ends, or once patience runs
// out.
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

// awaitReady waits until r reports ready. The coordinator's readiness does
// not wait on its services' checks, so a step that needs a reactor's source
// to be receiving waits for it here. It fails as await does.
func (c *coordinator) awaitReady(ctx context.Context, r corelifecycle.Component, what string) error {
	ready := make(chan struct{})
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		t := time.NewTicker(10 * time.Millisecond)
		defer t.Stop()
		for !r.Ready() {
			select {
			case <-stop:
				return
			case <-t.C:
			}
		}
		close(ready)
	}()
	return c.await(ctx, ready, what+" to be ready")
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
