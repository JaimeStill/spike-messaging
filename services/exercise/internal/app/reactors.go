package app

import (
	"context"
	"encoding/json"
	"fmt"
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

// ordersSubscription is the durable consumer and delivery group the orders
// reactor receives operations' orders through, and the consumer its claims
// are recorded under.
var ordersSubscription = messaging.Subscription{
	Name:  "exercise-orders",
	Types: []string{"operations.orders.issued"},
}

// Reactors composes the application's event-driven entry points: components
// that watch a source of occurrences and dispatch each one to a call, the
// inbound counterpart to a route. Relay publishes the outbox's committed
// events to the broker. Resolve resolves each running exercise's round when
// it is due. Orders records the orders operations issues.
type Reactors struct {
	Relay   *reactor.Reactor[event.Event]
	Resolve *reactor.Reactor[time.Time]
	Orders  *reactor.Reactor[event.Event]
}

// newReactors constructs the reactors and registers each on lc. It takes
// infra for the sources a reactor watches and dom for the domain calls it
// dispatches to — the two halves a reactor joins. A reactor that produces
// work sits at the root stage beside the server, so the drain stops it
// before the reactors that consume: the relay, and the resolver, which
// produces the rounds. The orders reactor consumes, at ordersStage. Each
// reactor's grace, half the shutdown timeout, stays below the
// coordinator's drain deadline, so a handler it cancels is reported before
// the deadline drops the report.
func newReactors(
	infra *Infrastructure,
	dom *Domain,
	cfg *config.Config,
	lc *lifecycle.Coordinator,
) (*Reactors, error) {
	grace := reactor.Grace(cfg.ShutdownTimeout.Duration() / 2)

	relay := reactor.New(
		infra.Outbox.Relay(infra.SQL.DB, outbox.Poll(cfg.Messaging.RelayPoll.Duration())),
		infra.Broker.Publish,
		grace,
	)
	register(lc, "relay", lifecycle.StageRoot, relay)

	// Every ends on a handler error, so the resolver logs a pass's failures
	// and returns nil: one exercise that fails to resolve never stops the
	// others' clock.
	resolve := reactor.New(reactor.Every(resolveTick), func(ctx context.Context, _ time.Time) error {
		if _, err := dom.Exercise.ResolveDue(ctx); err != nil {
			infra.Logger.ErrorContext(ctx, "resolve rounds", "error", err)
		}
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

// ordersIssued is the exercise service's reading of an
// operations.orders.issued event's data: its own type, decoded from the
// event, since the services share no Go types.
type ordersIssued struct {
	Exercise string        `json:"exercise"`
	Faction  string        `json:"faction"`
	Round    int           `json:"round"`
	Orders   []rules.Order `json:"orders"`
}

// recordOrders adapts an operations.orders.issued event into the domain's
// RecordOrders command, with a claim over the inbox bound to the event, so
// the event stops at the process boundary and a redelivery changes
// nothing. Data that does not decode is refused permanently: no
// redelivery can fix it.
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

// component is what a reactor offers the coordinator: the three lifecycle
// methods and the channel a failure after Start arrives on.
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
