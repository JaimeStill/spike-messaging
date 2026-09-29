package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/standards-lab/go-core/lifecycle"
	"github.com/standards-lab/sqlate"

	"github.com/JaimeStill/spike-messaging/core/event"
	"github.com/JaimeStill/spike-messaging/core/reactor"
	"github.com/JaimeStill/spike-messaging/messaging"
	"github.com/JaimeStill/spike-messaging/messaging/outbox"
	"github.com/JaimeStill/spike-messaging/services/exercise/domain/exercise"
	"github.com/JaimeStill/spike-messaging/services/exercise/domain/exercise/rules"
	"github.com/JaimeStill/spike-messaging/services/exercise/internal/config"
)

// resolveTick is how often the resolver looks for due rounds. A round is
// due at the database's clock, so the tick only bounds how late past its
// due time a round resolves.
const resolveTick = 50 * time.Millisecond

// ordersStage places the orders reactor above the domains' verification
// and below the root, so it receives only once the statements it runs are
// verified, and the drain stops it after the reactors that produce work.
const ordersStage = verifyStage + 1

// relayStage places the relay above every other reactor and below the root,
// so the drain stops it only after the server and the resolver, which
// commit events, and it publishes what they committed while it drained.
const relayStage = ordersStage + 1

// ordersSubscription names the durable consumer and delivery group through
// which the orders reactor receives the operations service's orders. Its
// Name is also the consumer the inbox records the reactor's claims under.
var ordersSubscription = messaging.Subscription{
	Name:  "exercise-orders",
	Types: []string{"operations.orders.issued"},
}

// Reactors composes the application's event-driven entry points: components
// that watch a source of occurrences and dispatch each one to a call, the
// inbound counterpart to a route. Relay publishes the outbox's committed
// events to the broker. Resolve resolves each running exercise's round when
// it is due. Orders records the orders the operations service issues.
type Reactors struct {
	Relay   *reactor.Reactor[event.Event]
	Resolve *reactor.Reactor[time.Time]
	Orders  *reactor.Reactor[event.Event]
}

// newReactors constructs the reactors and registers each on lc. It takes
// infra for the sources a reactor watches and dom for the domain calls it
// dispatches to — the two halves a reactor joins. The resolver produces
// the rounds, so it sits at the root stage beside the server, and the drain
// stops both first. The relay sits at relayStage, below them, so it drains
// after them and publishes the events they committed while they drained.
// The orders reactor consumes, so it sits at ordersStage, below the relay. Each reactor's grace, half the shutdown timeout, stays below
// the coordinator's drain deadline, so a handler the reactor cancels is
// reported before the deadline drops the report.
func newReactors(
	infra *Infrastructure,
	dom *Domain,
	cfg *config.Config,
	lc *lifecycle.Coordinator,
) (*Reactors, error) {
	shutdown := cfg.ShutdownTimeout.Duration()
	grace := reactor.Grace(shutdown / 2)

	// The relay's last pass, bounded below its grace, publishes what the
	// server and the resolver committed while they drained.
	relay := reactor.New(
		infra.Outbox.Relay(infra.SQL.DB,
			outbox.Poll(cfg.Messaging.RelayPoll.Duration()),
			outbox.Drain(shutdown/4)),
		infra.Broker.Publish,
		grace,
	)
	register(lc, "relay", relayStage, relay)

	// Every ends on a handler error, so the resolver logs a pass's failures
	// and returns nil: one exercise that fails to resolve never stops the
	// others' clock. A failure that persists, such as a database that is
	// down, is logged once per failureLogEvery rather than on every tick.
	failed := &failures{logger: infra.Logger, every: failureLogEvery, now: time.Now}
	resolve := reactor.New(reactor.Every(resolveTick), func(ctx context.Context, _ time.Time) error {
		_, err := dom.Exercise.ResolveDue(ctx)
		failed.report(ctx, "resolve rounds", err)
		return nil
	}, grace)
	register(lc, "resolve", lifecycle.StageRoot, resolve)

	src, err := infra.Broker.Subscribe(ordersSubscription)
	if err != nil {
		return nil, fmt.Errorf("orders: %w", err)
	}
	orders := reactor.New(src, recordOrders(dom.Exercise, infra), grace)
	register(lc, "orders", ordersStage, orders)

	return &Reactors{Relay: relay, Resolve: resolve, Orders: orders}, nil
}

// ordersIssued is the exercise service's own type for an
// operations.orders.issued event's data, decoded from the event, because
// the services share no Go types.
type ordersIssued struct {
	Exercise string        `json:"exercise"`
	Faction  string        `json:"faction"`
	Round    int           `json:"round"`
	Orders   []rules.Order `json:"orders"`
}

// recordOrders adapts an operations.orders.issued event into the domain's
// RecordOrders command, with a claim over the inbox bound to the event. The
// domain never sees the event, and a redelivery changes nothing. Data that
// does not decode is refused permanently, because no redelivery can fix
// it.
func recordOrders(svc *exercise.Service, infra *Infrastructure) reactor.Func[event.Event] {
	return func(ctx context.Context, e event.Event) error {
		var d ordersIssued
		if err := json.Unmarshal(e.Data, &d); err != nil {
			return event.Permanent(fmt.Errorf("orders %s: decode: %w", e.ID, err))
		}
		claim := func(ctx context.Context, tx *sqlate.Tx) (bool, error) {
			return infra.Inbox.Claim(ctx, tx, ordersSubscription.Name, e)
		}
		cmd := exercise.RecordOrders{Exercise: d.Exercise, Faction: d.Faction, Round: d.Round, Orders: d.Orders}
		if err := svc.RecordOrders(ctx, cmd, claim); err != nil {
			if event.IsPermanent(err) {
				infra.Logger.WarnContext(ctx, "orders refused", "event", e.ID, "error", err)
			}
			return err
		}
		return nil
	}
}

// failureLogEvery is how often a failure that repeats on every tick is
// logged while it persists.
const failureLogEvery = 10 * time.Second

// failures logs the outcome of a repeating operation without flooding the
// log: the first failure at once, then at most one line per every while
// failures persist, counting the ones it held back, and one line when the
// operation succeeds again. One goroutine reports to it, as a reactor's
// handler does, so it takes no lock.
type failures struct {
	logger     *slog.Logger
	every      time.Duration
	now        func() time.Time
	failing    bool
	last       time.Time
	suppressed int
}

// report records one outcome of the operation named msg: err, or nil for a
// success.
func (f *failures) report(ctx context.Context, msg string, err error) {
	now := f.now()
	switch {
	case err == nil && f.failing:
		f.logger.InfoContext(ctx, msg+" recovered", "suppressed", f.suppressed)
		f.failing, f.suppressed = false, 0
	case err == nil:
	case !f.failing || now.Sub(f.last) >= f.every:
		f.logger.ErrorContext(ctx, msg, "error", err, "suppressed", f.suppressed)
		f.failing, f.last, f.suppressed = true, now, 0
	default:
		f.suppressed++
	}
}

// component is what a reactor offers the coordinator: the three lifecycle
// methods, and the channel that reports a failure after Start.
type component interface {
	Start(context.Context) error
	Shutdown(context.Context) error
	Ready() bool
	Err() <-chan error
}

// register adds c to lc at stage under name, with its readiness check, and
// monitors its failures.
func register(lc *lifecycle.Coordinator, name string, stage int, c component) {
	lc.Add(lifecycle.Service{Name: name, Stage: stage, Start: c.Start, Shutdown: c.Shutdown, Check: c})
	lc.Monitor(c.Err())
}
