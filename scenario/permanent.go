package scenario

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/spf13/pflag"

	"github.com/JaimeStill/spike-messaging/core/event"
	"github.com/JaimeStill/spike-messaging/core/reactor"
	"github.com/JaimeStill/spike-messaging/messaging"
)

// permanentScenario terminates an event with a permanent error and watches
// that it never comes back, while the next event is handled normally.
func permanentScenario(brokers Brokers, needs []Need) Scenario {
	quiet := 500 * time.Millisecond
	const retry = 50 * time.Millisecond
	return Scenario{
		Name:    "permanent",
		Summary: "A permanent failure terminates the event instead of redelivering it",
		Needs:   needs,
		Flags: func(fs *pflag.FlagSet) {
			fs.DurationVar(&quiet, "quiet", quiet, "how long to watch for a redelivery that must not come")
		},
		Validate: func() error {
			if quiet <= retry {
				return fmt.Errorf("--quiet must exceed the %v retry delay, or a redelivery could go unseen", retry)
			}
			return nil
		},
		Steps: func() ([]Step, func() error) {
			c := newCoordinator(defaultDrain)
			next := newSignal()
			var b messaging.Broker
			var failed atomic.Int32
			return []Step{
				{
					Intent: fmt.Sprintf("Register a worker on subscription %q with a %v retry delay, and start the coordinator", group, retry),
					Action: func(ctx context.Context, rep *Reporter) error {
						var err error
						if b, err = brokers(); err != nil {
							return err
						}
						src, err := b.Subscribe(messaging.Subscription{Name: group, RetryDelay: retry})
						if err != nil {
							return err
						}
						handle := func(_ context.Context, e event.Event) error {
							if e.ID == "1" {
								rep.Note("event 1 delivery %d fails permanently", failed.Add(1))
								return event.Permanent(errors.New("event 1 cannot be handled"))
							}
							rep.Note("event %s handled", e.ID)
							next.fire()
							return nil
						}
						c.add("worker", 0, reactor.New(src, handle, reactor.Grace(defaultGrace)))
						return c.start(ctx, rep)
					},
				},
				{
					Intent: "Publish event 1, which fails permanently, then event 2",
					Action: func(ctx context.Context, rep *Reporter) error {
						for _, n := range []int{1, 2} {
							if err := publish(ctx, b, rep, numbered(n)); err != nil {
								return err
							}
						}
						return c.await(ctx, next.ch, "event 2")
					},
				},
				{
					Intent: fmt.Sprintf("Watch %v for a redelivery of event 1", quiet),
					Action: func(ctx context.Context, rep *Reporter) error {
						if !sleep(ctx, quiet) {
							return ctx.Err()
						}
						if n := failed.Load(); n != 1 {
							return fmt.Errorf("event 1 was delivered %d times, want once", n)
						}
						rep.Note("event 1 was delivered once and terminated")
						return nil
					},
				},
				{
					Intent: "Signal the drain and wait for the coordinator",
					Action: func(_ context.Context, rep *Reporter) error { return c.stop(rep) },
				},
			}, c.cleanup
		},
	}
}
