package scenario

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/spf13/pflag"
	"github.com/standards-lab/go-core/lifecycle"

	"github.com/JaimeStill/spike-messaging/core/reactor"
)

// everyScenario runs an interval reactor on the coordinator and drains it.
// A tick can be made to fail, which ends the reactor and so the run.
func everyScenario(needs []Need) Scenario {
	interval := 200 * time.Millisecond
	ticks := 3
	work := 100 * time.Millisecond
	drain, grace := defaultDrain, defaultGrace
	failAfter := 0
	return Scenario{
		Name:    "every",
		Summary: "An interval reactor on the lifecycle coordinator, drained on stop",
		Needs:   needs,
		Flags: func(fs *pflag.FlagSet) {
			fs.DurationVar(&interval, "interval", interval, "the tick interval")
			fs.IntVar(&ticks, "ticks", ticks, "how many ticks to handle before the drain")
			fs.DurationVar(&work, "work", work, "how long each tick's handling takes")
			fs.DurationVar(&drain, "drain", drain, "the coordinator's drain timeout")
			fs.DurationVar(&grace, "grace", grace, "how long a tick runs into the drain before it is cancelled, below --drain")
			fs.IntVar(&failAfter, "fail-after", failAfter, "fail on this tick, which ends the run (0 never fails)")
		},
		Validate: func() error {
			if interval <= 0 || ticks < 1 {
				return errors.New("--interval must be positive and --ticks at least 1")
			}
			return graceBelowDrain(grace, drain)
		},
		Steps: func() ([]Step, func() error) {
			c := newCoordinator(drain)
			handled := newSignal()
			var n, done atomic.Int32
			return []Step{
				{
					Intent: fmt.Sprintf("Register an Every(%v) reactor at the root stage, with a %v grace, and start the coordinator", interval, grace),
					Action: func(ctx context.Context, rep *Reporter) error {
						tick := func(ctx context.Context, _ time.Time) error {
							i := int(n.Add(1))
							rep.Note("tick %d started", i)
							if i == failAfter {
								rep.Note("tick %d failed", i)
								return fmt.Errorf("tick %d failed", i)
							}
							if !sleep(ctx, work) {
								rep.Note("tick %d cancelled: %v", i, ctx.Err())
								return ctx.Err()
							}
							rep.Note("tick %d done", i)
							if int(done.Add(1)) >= ticks {
								handled.fire()
							}
							return nil
						}
						r := reactor.New(reactor.Every(interval), tick, reactor.Grace(grace))
						c.add("ticker", lifecycle.StageRoot, r)
						return c.start(ctx, rep)
					},
				},
				{
					Intent: fmt.Sprintf("Let %d ticks run, each working %v", ticks, work),
					Action: func(ctx context.Context, _ *Reporter) error {
						return c.await(ctx, handled.ch, fmt.Sprintf("%d ticks", ticks))
					},
				},
				{
					Intent: "Signal the drain and wait for the coordinator",
					Action: func(_ context.Context, rep *Reporter) error {
						if err := c.stop(rep); err != nil {
							return err
						}
						rep.Note("%d ticks handled", done.Load())
						return nil
					},
				},
			}, c.cleanup
		},
	}
}
