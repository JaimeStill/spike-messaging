// Command every is step 1's checkpoint: an Every reactor on go-core's
// lifecycle coordinator, adapted by hand into a lifecycle.Service at the
// root stage and monitored through its Err channel. Interrupt it during a
// tick to watch the drain.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/standards-lab/go-core/lifecycle"
	"github.com/standards-lab/go-core/process"

	"github.com/JaimeStill/spike-messaging/core/reactor"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}

func run(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("every", flag.ContinueOnError)
	fs.SetOutput(stderr)
	interval := fs.Duration("interval", 500*time.Millisecond, "tick interval")
	work := fs.Duration("work", time.Second, "how long each tick's handling takes")
	drain := fs.Duration("drain", 5*time.Second, "drain timeout")
	failAfter := fs.Int("fail-after", 0, "fail on this tick (0 never fails)")
	if err := fs.Parse(args); err != nil {
		return process.ExitUsage
	}

	ctx, stop := process.SignalContext()
	defer stop()

	log := slog.New(slog.NewTextHandler(stderr, nil))
	n := 0
	tick := func(ctx context.Context, at time.Time) error {
		n++
		log.Info("tick start", "n", n, "at", at.Format(time.TimeOnly))
		if n == *failAfter {
			return fmt.Errorf("tick %d failed", n)
		}
		select {
		case <-time.After(*work):
			log.Info("tick done", "n", n)
			return nil
		case <-ctx.Done():
			log.Warn("tick cancelled", "n", n, "err", ctx.Err())
			return ctx.Err()
		}
	}

	r := reactor.New(reactor.Every(*interval), tick)

	lc := lifecycle.New()
	lc.Add(lifecycle.Service{
		Name:     "ticker",
		Stage:    lifecycle.StageRoot,
		Start:    r.Start,
		Shutdown: r.Shutdown,
		Check:    r,
	})
	lc.Monitor(r.Err())
	lc.OnReady(func() { log.Info("ready", "ticker", r.Ready()) })

	err := lc.Run(ctx, *drain)
	if err != nil {
		return process.Fail(stderr, "every", err)
	}
	log.Info("stopped cleanly")
	return process.ExitOK
}
