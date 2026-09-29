package app

import (
	"context"
	"time"

	"github.com/standards-lab/go-core/lifecycle"

	"github.com/JaimeStill/spike-messaging/core/event"
	corelifecycle "github.com/JaimeStill/spike-messaging/core/lifecycle"
	"github.com/JaimeStill/spike-messaging/core/reactor"
	"github.com/JaimeStill/spike-messaging/messaging"
	"github.com/JaimeStill/spike-messaging/services/operations/domain/operations"
	"github.com/JaimeStill/spike-messaging/services/operations/internal/config"
)

// relayStage places the relay above the domains' verification and below
// every stage that commits events, so the drain stops it last among the
// reactors, and its last pass publishes what they committed while they
// drained.
const relayStage = verifyStage + 1

// consumeStage places the consuming reactors above the relay: unlike
// exercise's, operations' consumers raise the service's events, so they
// produce, and the drain stops them before the relay.
const consumeStage = relayStage + 1

// retryDelay is how long an input for an operation not open yet waits
// before it is redelivered: the start that opens it arrives on another
// subscription, and is handled well within it.
const retryDelay = 250 * time.Millisecond

// The subscriptions, one per event type operations consumes, each a durable
// consumer and delivery group whose Name is also the consumer the inbox
// records its claims under. A replica joins each group, so replicas share
// every kind of input.
var (
	startedSubscription = messaging.Subscription{
		Name: "operations-started", Types: []string{"exercise.started"},
	}
	directivesSubscription = messaging.Subscription{
		Name: "operations-directives", Types: []string{"command.directive.issued"}, RetryDelay: retryDelay,
	}
	observedSubscription = messaging.Subscription{
		Name: "operations-observed", Types: []string{"exercise.round.observed"}, RetryDelay: retryDelay,
	}
	concludedSubscription = messaging.Subscription{
		Name: "operations-concluded", Types: []string{"exercise.concluded"}, RetryDelay: retryDelay,
	}
)

// Reactors composes the application's event-driven entry points: components
// that watch a source of occurrences and dispatch each one to a call, the
// inbound counterpart to a route. Relay publishes the outbox's committed
// events to the broker. Started, Directives, Observed, and Concluded invoke
// the domain's Open, Assign, Maneuver, and Close.
type Reactors struct {
	Relay      *reactor.Reactor[event.Event]
	Started    *reactor.Reactor[event.Event]
	Directives *reactor.Reactor[event.Event]
	Observed   *reactor.Reactor[event.Event]
	Concluded  *reactor.Reactor[event.Event]
}

// newReactors constructs the reactors and registers each on lc. It takes
// infra for the sources a reactor watches and dom for the domain calls it
// dispatches to — the two halves a reactor joins. Each consumer decodes its
// event's data straight into the command's input, operations' own reading
// of the payload, and hands the command a claim bound to the event.
func newReactors(
	infra *Infrastructure,
	dom *Domain,
	cfg *config.Config,
	lc *lifecycle.Coordinator,
) (*Reactors, error) {
	shutdown := cfg.ShutdownTimeout.Duration()
	svc := dom.Operations
	rs := &Reactors{Relay: infra.Messaging.Relay(infra.SQL.DB, shutdown)}
	corelifecycle.Register(lc, "relay", relayStage, rs.Relay)

	var err error
	if rs.Started, err = infra.Messaging.Consume(startedSubscription, shutdown, command(svc.Open)); err != nil {
		return nil, err
	}
	if rs.Directives, err = infra.Messaging.Consume(directivesSubscription, shutdown, command(svc.Assign)); err != nil {
		return nil, err
	}
	if rs.Observed, err = infra.Messaging.Consume(observedSubscription, shutdown, command(svc.Maneuver)); err != nil {
		return nil, err
	}
	if rs.Concluded, err = infra.Messaging.Consume(concludedSubscription, shutdown, command(svc.Close)); err != nil {
		return nil, err
	}
	corelifecycle.Register(lc, "started", consumeStage, rs.Started)
	corelifecycle.Register(lc, "directives", consumeStage, rs.Directives)
	corelifecycle.Register(lc, "observed", consumeStage, rs.Observed)
	corelifecycle.Register(lc, "concluded", consumeStage, rs.Concluded)
	return rs, nil
}

// command adapts a domain command, which takes the domain's own Claim, to
// the consumer a Consume call runs.
func command[T any](cmd func(context.Context, T, operations.Claim) error) func(context.Context, T, messaging.Claim) error {
	return func(ctx context.Context, c T, claim messaging.Claim) error {
		return cmd(ctx, c, claim)
	}
}
