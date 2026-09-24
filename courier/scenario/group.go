package scenario

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/spf13/pflag"
	"github.com/standards-lab/go-core/lifecycle"

	"github.com/JaimeStill/spike-messaging/core/event"
	"github.com/JaimeStill/spike-messaging/core/reactor"
	"github.com/JaimeStill/spike-messaging/messaging"
)

// groupScenario runs a publisher reactor and two workers that share one
// subscription, and checks that the workers split the events.
func groupScenario(brokers Brokers, needs []Need) Scenario {
	events := 12
	interval := 50 * time.Millisecond
	work := 100 * time.Millisecond
	return Scenario{
		Name:    "group",
		Summary: "Two workers in one delivery group split the published events",
		Needs:   needs,
		Flags: func(fs *pflag.FlagSet) {
			fs.IntVar(&events, "events", events, "how many events the publisher publishes")
			fs.DurationVar(&interval, "interval", interval, "how often the publisher publishes")
			fs.DurationVar(&work, "work", work, "how long each handling takes")
		},
		Validate: func() error {
			if events < 2 || interval <= 0 {
				return errors.New("--events must be at least 2 and --interval positive")
			}
			return nil
		},
		Steps: func() ([]Step, func() error) {
			c := newCoordinator(defaultDrain)
			all := newSignal()
			var mu sync.Mutex
			by := map[string][]string{} // event to the workers that handled it
			return []Step{
				{
					Intent: fmt.Sprintf("Register a publisher at the root stage and two workers on subscription %q at stage 0, then start the coordinator", group),
					Action: func(ctx context.Context, rep *Reporter) error {
						b, err := brokers()
						if err != nil {
							return err
						}
						published := 0
						pub := reactor.New(reactor.Every(interval), func(ctx context.Context, _ time.Time) error {
							if published == events {
								return nil
							}
							published++
							return publish(ctx, b, rep, numbered(published))
						})
						c.add("publisher", lifecycle.StageRoot, pub)
						for _, name := range []string{"worker-a", "worker-b"} {
							src, err := b.Subscribe(messaging.Subscription{Name: group})
							if err != nil {
								return err
							}
							handle := func(ctx context.Context, e event.Event) error {
								rep.Note("%s handling event %s", name, e.ID)
								if !sleep(ctx, work) {
									return ctx.Err()
								}
								mu.Lock()
								by[e.ID] = append(by[e.ID], name)
								if len(by) == events {
									all.fire()
								}
								mu.Unlock()
								return nil
							}
							c.add(name, 0, reactor.New(src, handle, reactor.Grace(defaultGrace)))
						}
						return c.start(ctx, rep)
					},
				},
				{
					Intent: fmt.Sprintf("Publish %d events, one every %v, and let the group handle them", events, interval),
					Action: func(ctx context.Context, _ *Reporter) error {
						return c.await(ctx, all.ch, fmt.Sprintf("all %d events", events))
					},
				},
				{
					Intent: "Signal the drain: the publisher stops first, then the workers",
					Action: func(_ context.Context, rep *Reporter) error { return c.stop(rep) },
				},
				{
					Intent: "Check that each event was handled once and both workers shared the work",
					Action: func(_ context.Context, rep *Reporter) error {
						mu.Lock()
						defer mu.Unlock()
						share := map[string][]string{}
						var errs []error
						for id, workers := range by {
							if len(workers) != 1 {
								errs = append(errs, fmt.Errorf("event %s handled %d times", id, len(workers)))
							}
							share[workers[0]] = append(share[workers[0]], id)
						}
						for _, name := range []string{"worker-a", "worker-b"} {
							ids := share[name]
							slices.SortFunc(ids, byNumber)
							rep.Note("%s handled %d: %s", name, len(ids), strings.Join(ids, " "))
							if len(ids) == 0 {
								errs = append(errs, fmt.Errorf("%s handled nothing", name))
							}
						}
						return errors.Join(errs...)
					},
				},
			}, c.cleanup
		},
	}
}

func byNumber(a, b string) int {
	if len(a) != len(b) {
		return len(a) - len(b)
	}
	return strings.Compare(a, b)
}
