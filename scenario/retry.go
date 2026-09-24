package scenario

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/spf13/pflag"

	"github.com/JaimeStill/spike-messaging/core/event"
	"github.com/JaimeStill/spike-messaging/core/reactor"
	"github.com/JaimeStill/spike-messaging/messaging"
)

// retryScenario fails an event's first delivery and watches it come back
// after the subscription's retry delay.
func retryScenario(brokers Brokers, needs []Need) Scenario {
	retry := 300 * time.Millisecond
	return Scenario{
		Name:    "retry",
		Summary: "A failed delivery is redelivered after the retry delay",
		Needs:   needs,
		Flags: func(fs *pflag.FlagSet) {
			fs.DurationVar(&retry, "retry", retry, "how long a failed event waits before redelivery")
		},
		Validate: func() error {
			if retry <= 0 {
				return errors.New("--retry must be positive")
			}
			return nil
		},
		Steps: func() ([]Step, func() error) {
			c := newCoordinator(defaultDrain)
			redelivered := newSignal()
			var b messaging.Broker
			var mu sync.Mutex
			var attempts []time.Time
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
							mu.Lock()
							defer mu.Unlock()
							attempts = append(attempts, time.Now())
							if n := len(attempts); n > 1 {
								rep.Note("event %s attempt %d handled, %v after the failure", e.ID, n, attempts[n-1].Sub(attempts[n-2]).Round(time.Millisecond))
								redelivered.fire()
								return nil
							}
							rep.Note("event %s attempt 1 failed; it will be redelivered", e.ID)
							return fmt.Errorf("event %s: transient failure", e.ID)
						}
						c.add("worker", 0, reactor.New(src, handle, reactor.Grace(defaultGrace)))
						return c.start(ctx)
					},
				},
				{
					Intent: "Publish one event whose first delivery fails, and wait for the redelivery",
					Action: func(ctx context.Context, rep *Reporter) error {
						if err := publish(ctx, b, rep, numbered(1)); err != nil {
							return err
						}
						return c.await(ctx, redelivered.ch, "the redelivery")
					},
				},
				{
					Intent: "Signal the drain and wait for the coordinator",
					Action: func(context.Context, *Reporter) error { return c.stop() },
				},
				{
					Intent: fmt.Sprintf("Check the event was handled twice, the redelivery no sooner than %v", retry),
					Action: func(_ context.Context, rep *Reporter) error {
						mu.Lock()
						defer mu.Unlock()
						if len(attempts) != 2 {
							return fmt.Errorf("handled %d times, want 2", len(attempts))
						}
						if gap := attempts[1].Sub(attempts[0]); gap < retry {
							return fmt.Errorf("redelivered after %v, sooner than the %v retry delay", gap, retry)
						}
						rep.Note("two deliveries, the second after the retry delay")
						return nil
					},
				},
			}, c.cleanup
		},
	}
}
