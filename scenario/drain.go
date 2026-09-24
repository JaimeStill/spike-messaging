package scenario

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/spf13/pflag"

	"github.com/JaimeStill/spike-messaging/core/event"
	"github.com/JaimeStill/spike-messaging/core/reactor"
	"github.com/JaimeStill/spike-messaging/messaging"
)

// drainScenario signals the drain while an event is being handled. Handling
// shorter than the grace finishes, and its acknowledgement holds; longer, the
// reactor cancels it and the run fails with the reactor's report.
func drainScenario(brokers Brokers, needs []Need) Scenario {
	work := 2 * time.Second
	drain, grace := defaultDrain, defaultGrace
	return Scenario{
		Name:    "drain",
		Summary: "A drain lets in-flight handling finish, or cancels it after the grace",
		Needs:   needs,
		Flags: func(fs *pflag.FlagSet) {
			fs.DurationVar(&work, "work", work, "how long the in-flight handling takes")
			fs.DurationVar(&drain, "drain", drain, "the coordinator's drain timeout")
			fs.DurationVar(&grace, "grace", grace, "how long handling runs into the drain before it is cancelled, below --drain")
		},
		Validate: func() error { return graceBelowDrain(grace, drain) },
		Steps: func() ([]Step, func() error) {
			c := newCoordinator(drain)
			started := newSignal()
			var b messaging.Broker
			sub := messaging.Subscription{Name: group}
			return []Step{
				{
					Intent: fmt.Sprintf("Register a worker with a %v grace under a %v drain timeout, and start the coordinator", grace, drain),
					Action: func(ctx context.Context, rep *Reporter) error {
						var err error
						if b, err = brokers(); err != nil {
							return err
						}
						src, err := b.Subscribe(sub)
						if err != nil {
							return err
						}
						handle := func(ctx context.Context, e event.Event) error {
							rep.Note("event %s handling started, %v of work", e.ID, work)
							started.fire()
							if !sleep(ctx, work) {
								rep.Note("event %s handling cancelled: %v", e.ID, ctx.Err())
								return ctx.Err()
							}
							rep.Note("event %s handling finished", e.ID)
							return nil
						}
						c.add("worker", 0, reactor.New(src, handle, reactor.Grace(grace)))
						return c.start(ctx, rep)
					},
				},
				{
					Intent: "Publish one event and wait for its handling to begin",
					Action: func(ctx context.Context, rep *Reporter) error {
						if err := publish(ctx, b, rep, numbered(1)); err != nil {
							return err
						}
						return c.await(ctx, started.ch, "the handling to begin")
					},
				},
				{
					Intent: "Signal the drain while the event is in flight",
					Action: func(_ context.Context, rep *Reporter) error {
						return c.stop(rep)
					},
				},
				{
					Intent: fmt.Sprintf("Start a new member of %q and check the drained event is not redelivered", group),
					Action: func(ctx context.Context, rep *Reporter) error {
						return checkAcknowledged(ctx, b, sub, rep)
					},
				},
			}, c.cleanup
		},
	}
}

// checkAcknowledged runs a fresh member of sub until it handles a new event,
// and fails if event 1 reaches it first.
func checkAcknowledged(ctx context.Context, b messaging.Broker, sub messaging.Subscription, rep *Reporter) error {
	src, err := b.Subscribe(sub)
	if err != nil {
		return err
	}
	var redelivered atomic.Bool
	seen := newSignal()
	r := reactor.New(src, func(_ context.Context, e event.Event) error {
		if e.ID == "1" {
			redelivered.Store(true)
		}
		seen.fire()
		return nil
	})
	if err := r.Start(ctx); err != nil {
		return err
	}
	defer func() {
		sctx, cancel := context.WithTimeout(context.Background(), defaultDrain)
		defer cancel()
		_ = r.Shutdown(sctx)
	}()
	if err := publish(ctx, b, rep, numbered(2)); err != nil {
		return err
	}
	select {
	case <-seen.ch:
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(patience):
		return fmt.Errorf("timed out waiting for the new member")
	}
	if redelivered.Load() {
		return fmt.Errorf("event 1 was redelivered: its acknowledgement was lost")
	}
	rep.Note("the new member received event 2 first: event 1's acknowledgement held")
	return nil
}
