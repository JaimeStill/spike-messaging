package scenario

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"uuid"

	"github.com/spf13/pflag"

	corelifecycle "github.com/JaimeStill/spike-messaging/core/lifecycle"
	"github.com/JaimeStill/spike-messaging/core/reactor"
	"github.com/JaimeStill/spike-messaging/messaging"
)

// TheaterDurable prefixes the durable consumer each theater run subscribes
// under, so the stream's consumers show which are courier's.
const TheaterDurable = "courier-theater-"

// settleDelay is how long the theater waits, after the conclusion, for the
// final round's assessments, which are issued after it, before it narrates
// what remains without them.
const settleDelay = 3 * time.Second

// theaterScenario joins the exercise services' stream and narrates one
// exercise as a demonstration. It narrates the initial conditions, then
// each round as a block once the round is complete, by side: what the
// observer recorded, then what each faction knows, decides, and orders.
// It closes with the final conditions, with a ledger of the stream's
// traffic and the chain's latency.
func theaterScenario(joins Joins, needs func() []Need) Scenario {
	var exercise, exerciseURL, stream, prefix string
	maxAge := 24 * time.Hour
	wait := 2 * time.Minute
	return Scenario{
		Name:    "theater",
		Summary: "Narrate an exercise as the services play it: initial conditions, what each round changed, by side, and the final conditions",
		Needs:   needs,
		Flags: func(fs *pflag.FlagSet) {
			fs.StringVar(&exercise, "exercise", "", "the ID of the exercise to narrate, joined before it starts (required)")
			fs.StringVar(&exerciseURL, "exercise-url", "http://localhost:8081", "the exercise service's base URL, which tells the seed and the objectives")
			fs.StringVar(&stream, "stream", "exercise", "the stream the exercise services share")
			fs.StringVar(&prefix, "prefix", "exercise", "the subject prefix of the services' stream")
			fs.DurationVar(&maxAge, "max-age", maxAge, "the stream's max_age, as the services configure it: the last broker to provision the stream sets it")
			fs.DurationVar(&wait, "wait", wait, "how long to wait for the exercise to conclude")
		},
		Validate: func() error {
			var errs []error
			if _, err := uuid.Parse(exercise); err != nil {
				errs = append(errs, errors.New("--exercise must be an exercise's ID"))
			}
			if err := validExerciseURL(exerciseURL); err != nil {
				errs = append(errs, err)
			}
			if wait <= 0 {
				errs = append(errs, errors.New("--wait must be positive"))
			}
			return errors.Join(errs...)
		},
		Steps: func() ([]Step, func() error) {
			c := newCoordinator(defaultDrain)
			var l lease
			n := newNarrator(exercise)
			n.read = func(ctx context.Context) (exerciseView, error) { return readExercise(ctx, exerciseURL, exercise) }
			return []Step{
				{
					Intent: fmt.Sprintf("Join the %s stream, and wait for exercise %s to start", stream, exercise),
					Action: func(ctx context.Context, rep *Reporter) error {
						if joins == nil {
							return errors.New("no stream to join is configured")
						}
						b, release, err := joins(stream, prefix, maxAge)
						if err != nil {
							return err
						}
						l.release = release
						src, err := b.Subscribe(messaging.Subscription{
							Name: TheaterDurable + strings.ReplaceAll(uuid.NewV7().String(), "-", ""),
							Types: []string{
								startedType, resolvedType, lostType, observedType, assessmentType,
								directiveType, ordersType, concludedType,
							},
						})
						if err != nil {
							return err
						}
						n.note = rep.Note
						corelifecycle.Register(c.lc, "watch", 0, reactor.New(src, n.handle, reactor.Grace(defaultGrace)))
						if err := c.start(ctx, rep); err != nil {
							return err
						}
						wctx, cancel := context.WithTimeout(ctx, wait)
						defer cancel()
						select {
						case <-n.started.ch:
							return nil
						case <-wctx.Done():
							return fmt.Errorf("exercise %s did not start within %s", exercise, wait)
						}
					},
				},
				{
					Intent: "Narrate events until exercise completion",
					Action: func(ctx context.Context, _ *Reporter) error {
						n.begin()
						wctx, cancel := context.WithTimeout(ctx, wait)
						defer cancel()
						select {
						case <-n.concluded.ch:
						case <-wctx.Done():
							return fmt.Errorf("exercise %s did not conclude within %s", exercise, wait)
						}
						// The final round's assessments, and the revisions of those
						// of a faction that lost an objective in it, follow the
						// conclusion.
						select {
						case <-n.settled.ch:
						case <-time.After(settleDelay):
						case <-ctx.Done():
							return ctx.Err()
						}
						n.finish()
						return nil
					},
				},
				{
					Intent: "Skirmish complete, results",
					Action: func(_ context.Context, rep *Reporter) error {
						// Events can arrive after the step before finishes:
						// narrate the blocks they complete before the results.
						n.finish()
						for _, line := range n.conditions() {
							rep.Note("%s", line)
						}
						return nil
					},
				},
				{
					Intent: "Signal the drain: the watch stops receiving",
					Action: func(_ context.Context, rep *Reporter) error { return c.stop(rep) },
				},
			}, afterDrain(c, &l)
		},
	}
}
