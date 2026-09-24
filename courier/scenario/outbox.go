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
	"github.com/standards-lab/sqlate"

	"github.com/JaimeStill/spike-messaging/core/event"
	"github.com/JaimeStill/spike-messaging/core/reactor"
	"github.com/JaimeStill/spike-messaging/messaging"
	"github.com/JaimeStill/spike-messaging/messaging/outbox"
)

// OutboxStore is an outbox over a database of its own, ready for one run:
// the messaging tables are migrated and the outbox's statements verified.
type OutboxStore struct {
	Name   string          // the database, for the narration
	DB     sqlate.Beginner // the session the emit calls and the relay run on
	Outbox *outbox.Outbox
	// Pending counts the outbox rows the relay has not yet published. The
	// count is the engine's SQL, so the composition root supplies it.
	Pending func(context.Context) (int, error)
	// Close releases the store and removes its database.
	Close func() error
}

// Outboxes builds a fresh outbox store for one scenario run.
type Outboxes func(context.Context) (*OutboxStore, error)

// outboxScenario commits events while no relay runs, then starts a relay and
// a worker, and checks that every event reaches the worker once and no row
// is left pending: evidence 2 on a real database.
func outboxScenario(brokers Brokers, outboxes Outboxes, needs []Need) Scenario {
	events := 5
	poll := 200 * time.Millisecond
	return Scenario{
		Name:    "outbox",
		Summary: "Events committed with no relay running are all published once a relay runs",
		Needs:   needs,
		Flags: func(fs *pflag.FlagSet) {
			fs.IntVar(&events, "events", events, "how many events are committed before the relay starts")
			fs.DurationVar(&poll, "poll", poll, "how long the relay waits between passes once it finds no row")
		},
		Validate: func() error {
			if events < 1 || poll <= 0 {
				return errors.New("--events must be at least 1 and --poll positive")
			}
			return nil
		},
		Steps: func() ([]Step, func() error) {
			c := newCoordinator(defaultDrain)
			all := newSignal()
			var store *OutboxStore
			var storeRep *Reporter
			var mu sync.Mutex
			delivered := map[string]int{} // event id to its delivery count
			cleanup := func() error {
				err := c.cleanup()
				if store != nil {
					if cerr := store.Close(); cerr != nil {
						return errors.Join(err, fmt.Errorf("drop database %s: %w", store.Name, cerr))
					}
					storeRep.Note("dropped database %s", store.Name)
				}
				return err
			}
			return []Step{
				{
					Intent: "Create a scratch database, migrate the messaging tables, and verify the outbox's statements",
					Action: func(ctx context.Context, rep *Reporter) error {
						if outboxes == nil {
							return errors.New("no outbox store is configured")
						}
						var err error
						if store, err = outboxes(ctx); err != nil {
							return err
						}
						storeRep = rep
						rep.Note("database %s ready", store.Name)
						return nil
					},
				},
				{
					Intent: fmt.Sprintf("Emit %d events, each in its own transaction, with no relay running", events),
					Action: func(ctx context.Context, rep *Reporter) error {
						em := store.Outbox.Emitter()
						for n := 1; n <= events; n++ {
							e := numbered(n)
							if _, err := sqlate.Transact(ctx, store.DB, func(tx *sqlate.Tx) (struct{}, error) {
								return struct{}{}, em.Emit(ctx, tx, e)
							}); err != nil {
								return fmt.Errorf("emit %s: %w", e.ID, err)
							}
							rep.Note("committed event %s", e.ID)
						}
						pending, err := store.Pending(ctx)
						if err != nil {
							return err
						}
						rep.Note("%d rows pending", pending)
						return nil
					},
				},
				{
					Intent: fmt.Sprintf("Register a relay at the root stage and a worker on subscription %q at stage 0, start the coordinator, and wait for every delivery", group),
					Action: func(ctx context.Context, rep *Reporter) error {
						b, err := brokers()
						if err != nil {
							return err
						}
						src, err := b.Subscribe(messaging.Subscription{Name: group})
						if err != nil {
							return err
						}
						handle := func(_ context.Context, e event.Event) error {
							rep.Note("delivered event %s", e.ID)
							mu.Lock()
							defer mu.Unlock()
							delivered[e.ID]++
							if len(delivered) == events {
								all.fire()
							}
							return nil
						}
						c.add("worker", 0, reactor.New(src, handle, reactor.Grace(defaultGrace)))
						relay := store.Outbox.Relay(store.DB, outbox.Poll(poll))
						c.add("relay", lifecycle.StageRoot, reactor.New(relay, b.Publish, reactor.Grace(defaultGrace)))
						if err := c.start(ctx, rep); err != nil {
							return err
						}
						return c.await(ctx, all.ch, fmt.Sprintf("all %d deliveries", events))
					},
				},
				{
					Intent: "Signal the drain: the relay stops first, then the worker",
					Action: func(_ context.Context, rep *Reporter) error { return c.stop(rep) },
				},
				{
					Intent: "Check that each event was delivered once and no row is left pending",
					Action: func(ctx context.Context, rep *Reporter) error {
						mu.Lock()
						var errs []error
						var ids []string
						for id, n := range delivered {
							ids = append(ids, id)
							if n != 1 {
								errs = append(errs, fmt.Errorf("event %s delivered %d times", id, n))
							}
						}
						mu.Unlock()
						slices.SortFunc(ids, byNumber)
						if len(ids) != events {
							errs = append(errs, fmt.Errorf("%d events delivered, want %d", len(ids), events))
						}
						if len(errs) == 0 {
							rep.Note("each of %d events delivered once: %s", events, strings.Join(ids, " "))
						}
						pending, err := store.Pending(ctx)
						if err != nil {
							return errors.Join(append(errs, err)...)
						}
						rep.Note("%d rows pending", pending)
						if pending != 0 {
							errs = append(errs, fmt.Errorf("%d rows left pending", pending))
						}
						return errors.Join(errs...)
					},
				},
			}, cleanup
		},
	}
}
