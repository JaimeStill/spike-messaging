// Command group is the messaging step's checkpoint: a delivery group on the
// memory broker, run by go-core's lifecycle coordinator. A publisher reactor
// at the root stage publishes a numbered event on every tick, and two worker
// reactors at stage 0 share one subscription, so they split the events. Each
// reactor is adapted by hand into a lifecycle.Service and monitored through
// its Err channel. The drain stops the publisher first, then lets the
// workers finish; interrupt it during a handling to watch the drain.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/standards-lab/go-core/lifecycle"
	"github.com/standards-lab/go-core/process"

	"github.com/JaimeStill/spike-messaging/core/event"
	"github.com/JaimeStill/spike-messaging/core/reactor"
	"github.com/JaimeStill/spike-messaging/messaging"
	"github.com/JaimeStill/spike-messaging/messaging/memory"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}

func run(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("group", flag.ContinueOnError)
	fs.SetOutput(stderr)
	interval := fs.Duration("interval", 500*time.Millisecond, "how often the publisher publishes an event")
	work := fs.Duration("work", 300*time.Millisecond, "how long each handling takes")
	failEvery := fs.Int("fail-every", 0, "fail the first delivery of every event whose number divides by this (0 never fails)")
	permanent := fs.Int("permanent", 0, "terminate this event with a permanent error (0 never does)")
	retry := fs.Duration("retry", 500*time.Millisecond, "how long a failed event waits before redelivery")
	drain := fs.Duration("drain", 5*time.Second, "the coordinator's drain timeout")
	grace := fs.Duration("grace", 4*time.Second, "how long handling runs into the drain before it is cancelled, below -drain")
	if err := fs.Parse(args); err != nil {
		return process.ExitUsage
	}
	if *grace >= *drain {
		return process.Usage(stderr, "group: -grace must be below -drain, or the coordinator's timeout hides the reactor's report")
	}

	ctx, stop := process.SignalContext()
	defer stop()

	log := slog.New(slog.NewTextHandler(stderr, nil))
	broker := memory.New()

	n := 0
	publisher := reactor.New(reactor.Every(*interval), func(ctx context.Context, _ time.Time) error {
		n++
		e := event.Event{ID: strconv.Itoa(n), Source: "/cmd/group", Type: "lab.demo.tick", Time: time.Now()}
		if err := broker.Publish(ctx, e); err != nil {
			return err
		}
		log.Info("published", "id", e.ID)
		return nil
	})

	var mu sync.Mutex
	attempts := map[string]int{}
	handle := func(worker string) reactor.Func[event.Event] {
		return func(ctx context.Context, e event.Event) error {
			mu.Lock()
			attempts[e.ID]++
			attempt := attempts[e.ID]
			mu.Unlock()
			num, _ := strconv.Atoi(e.ID)
			log := log.With("worker", worker, "id", e.ID, "attempt", attempt)

			if num == *permanent {
				log.Warn("terminated")
				return event.Permanent(errors.New("event cannot be handled"))
			}
			if *failEvery > 0 && num%*failEvery == 0 && attempt == 1 {
				log.Warn("failed; will be redelivered")
				return fmt.Errorf("event %s: transient failure", e.ID)
			}
			log.Info("handling")
			select {
			case <-time.After(*work):
				log.Info("handled")
				return nil
			case <-ctx.Done():
				log.Warn("handling cancelled", "err", ctx.Err())
				return ctx.Err()
			}
		}
	}

	sub := messaging.Subscription{Name: "workers", RetryDelay: *retry}
	lc := lifecycle.New()
	lc.Add(lifecycle.Service{
		Name:     "publisher",
		Stage:    lifecycle.StageRoot,
		Start:    publisher.Start,
		Shutdown: publisher.Shutdown,
		Check:    publisher,
	})
	lc.Monitor(publisher.Err())
	for _, name := range []string{"worker-a", "worker-b"} {
		src, err := broker.Subscribe(sub)
		if err != nil {
			return process.Fail(stderr, "group", err)
		}
		w := reactor.New(src, handle(name), reactor.Grace(*grace))
		lc.Add(lifecycle.Service{
			Name:     name,
			Stage:    0,
			Start:    w.Start,
			Shutdown: w.Shutdown,
			Check:    w,
		})
		lc.Monitor(w.Err())
	}
	lc.OnReady(func() { log.Info("ready") })

	if err := lc.Run(ctx, *drain); err != nil {
		return process.Fail(stderr, "group", err)
	}
	log.Info("stopped cleanly")
	return process.ExitOK
}
