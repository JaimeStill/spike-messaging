package app

import (
	"context"
	"time"

	"github.com/standards-lab/go-core/lifecycle"

	"github.com/JaimeStill/spike-messaging/core/event"
	corelifecycle "github.com/JaimeStill/spike-messaging/core/lifecycle"
	"github.com/JaimeStill/spike-messaging/core/logging"
	"github.com/JaimeStill/spike-messaging/core/reactor"
	"github.com/JaimeStill/spike-messaging/messaging"
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
// The orders reactor consumes, so it sits at ordersStage, below the relay.
// Each reactor's grace, which reactor.GraceWithin derives from the shutdown
// timeout, stays below the coordinator's drain deadline.
func newReactors(
	infra *Infrastructure,
	dom *Domain,
	cfg *config.Config,
	lc *lifecycle.Coordinator,
) (*Reactors, error) {
	shutdown := cfg.ShutdownTimeout.Duration()

	relay := infra.Messaging.Relay(infra.SQL.DB, shutdown)
	corelifecycle.Register(lc, "relay", relayStage, relay)

	// Every ends on a handler error, so the resolver logs a pass's failures
	// and returns nil: one exercise that fails to resolve never stops the
	// others' clock. A failure that persists, such as a database that is
	// down, is logged once per failureLogEvery rather than on every tick.
	failed := logging.NewThrottle(infra.Logger, failureLogEvery)
	resolve := reactor.New(reactor.Every(resolveTick), func(ctx context.Context, _ time.Time) error {
		_, err := dom.Exercise.ResolveDue(ctx)
		failed.Report(ctx, "resolve rounds", err)
		return nil
	}, reactor.GraceWithin(shutdown))
	corelifecycle.Register(lc, "resolve", lifecycle.StageRoot, resolve)

	orders, err := infra.Messaging.Consume(ordersSubscription, shutdown, recordOrders(dom.Exercise))
	if err != nil {
		return nil, err
	}
	corelifecycle.Register(lc, "orders", ordersStage, orders)

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

// recordOrders adapts an operations.orders.issued event's data into the
// domain's RecordOrders command, with the claim the reactor bound to the
// event, so the domain never sees the event and a redelivery changes
// nothing. A refusal is permanent, and the runtime logs it.
func recordOrders(svc *exercise.Service) func(context.Context, ordersIssued, messaging.Claim) error {
	return func(ctx context.Context, d ordersIssued, claim messaging.Claim) error {
		cmd := exercise.RecordOrders{Exercise: d.Exercise, Faction: d.Faction, Round: d.Round, Orders: d.Orders}
		return svc.RecordOrders(ctx, cmd, claim)
	}
}

// failureLogEvery is how often a failure that repeats on every tick is
// logged while it persists.
const failureLogEvery = 10 * time.Second
