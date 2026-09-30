package scenario

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/pflag"
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
	j := newJoinFlags(2 * time.Minute)
	var exerciseURL string
	return Scenario{
		Name:    "theater",
		Summary: "Narrate an exercise as the services play it: initial conditions, what each round changed, by side, and the final conditions",
		Needs:   needs,
		Flags: func(fs *pflag.FlagSet) {
			j.bind(fs, "the ID of the exercise to narrate, joined before it starts (required)", "how long to wait for the exercise to conclude")
			fs.StringVar(&exerciseURL, "exercise-url", "http://localhost:8081", "the exercise service's base URL, which tells the seed and the objectives")
		},
		Validate: func() error { return j.validate(validExerciseURL(exerciseURL)) },
		Steps: func() ([]Step, func() error) {
			c := newCoordinator(defaultDrain)
			var l lease
			n := newNarrator(j.exercise)
			n.read = func(ctx context.Context) (exerciseView, error) { return readExercise(ctx, exerciseURL, j.exercise) }
			return []Step{
				{
					Intent: fmt.Sprintf("Join the %s stream, and wait for exercise %s to start", j.stream, j.exercise),
					Action: func(ctx context.Context, rep *Reporter) error {
						n.note = rep.Note
						_, err := l.join(ctx, rep, c, joins, j, TheaterDurable, n.handle,
							startedType, resolvedType, lostType, observedType, assessmentType,
							directiveType, ordersType, concludedType)
						if err != nil {
							return err
						}
						wctx, cancel := context.WithTimeout(ctx, j.wait)
						defer cancel()
						select {
						case <-n.started.ch:
							return nil
						case <-wctx.Done():
							return fmt.Errorf("exercise %s did not start within %s", j.exercise, j.wait)
						}
					},
				},
				{
					Intent: "Narrate events until exercise completion",
					Action: func(ctx context.Context, _ *Reporter) error {
						n.begin()
						wctx, cancel := context.WithTimeout(ctx, j.wait)
						defer cancel()
						select {
						case <-n.concluded.ch:
						case <-wctx.Done():
							return fmt.Errorf("exercise %s did not conclude within %s", j.exercise, j.wait)
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
				stopWatch(c),
			}, afterDrain(c, &l)
		},
	}
}
