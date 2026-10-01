package app

import (
	"time"

	"github.com/standards-lab/go-core/lifecycle"

	"github.com/JaimeStill/spike-messaging/core/event"
	corelifecycle "github.com/JaimeStill/spike-messaging/core/lifecycle"
	"github.com/JaimeStill/spike-messaging/core/reactor"
	"github.com/JaimeStill/spike-messaging/messaging"
	"github.com/JaimeStill/spike-messaging/services/command/internal/config"
)

// relayStage places the relay above the domains' verification and below
// every stage that commits events, so the drain stops it last among the
// reactors, and its last pass publishes what they committed while they
// drained.
const relayStage = verifyStage + 1

// consumeStage places the consuming reactors above the relay. Their
// commands raise events, so the drain stops the consumers before the relay
// and the relay publishes what they committed.
const consumeStage = relayStage + 1

// retryDelay is how long an input for a direction not open yet waits
// before its redelivery. The start that opens the direction arrives on
// another subscription and is handled well within that time.
const retryDelay = 250 * time.Millisecond

// maxDeliver bounds the deliveries of an input that may arrive before its
// start: at retryDelay, about a minute of redeliveries. An input for a
// direction that never opens, such as one for an exercise that began before
// the service's first boot, then stops rather than retrying without end. The
// bound applies to every failure, so an outage of the service's database
// longer than it drops the input.
const maxDeliver = 240

// The subscriptions, one per event type the service consumes. Each is a
// durable consumer and delivery group, and its Name is also the consumer the
// inbox records its claims under. A replica joins each group, so replicas
// share every kind of input. Each starts at its consumer's creation, so a
// first boot skips the events the stream already retains, and an exercise
// already under way then is not played: its inputs find no open direction
// and stop at maxDeliver. Start and MaxDeliver are part of the consumer's
// configuration, so changing either fails the bind on a stream where the
// durable exists; recreate it, as mise run reset does.
var (
	startedSubscription = messaging.Subscription{
		Name: "command-started", Types: []string{"exercise.started"}, Start: messaging.StartNew,
	}
	assessedSubscription = messaging.Subscription{
		Name: "command-assessed", Types: []string{"intelligence.assessment.issued"}, RetryDelay: retryDelay,
		MaxDeliver: maxDeliver, Start: messaging.StartNew,
	}
	concludedSubscription = messaging.Subscription{
		Name: "command-concluded", Types: []string{"exercise.concluded"}, RetryDelay: retryDelay,
		MaxDeliver: maxDeliver, Start: messaging.StartNew,
	}
)

// Reactors composes the application's event-driven entry points: components
// that watch a source of occurrences and dispatch each one to a call, the
// inbound counterpart to a route. Relay publishes the outbox's committed
// events to the broker. Started, Assessed, and Concluded invoke the
// domain's Open, Decide, and Close.
type Reactors struct {
	Relay     *reactor.Reactor[event.Event]
	Started   *reactor.Reactor[event.Event]
	Assessed  *reactor.Reactor[event.Event]
	Concluded *reactor.Reactor[event.Event]
}

// newReactors constructs the reactors and registers each on lc. It takes
// infra for the sources a reactor watches and dom for the domain calls it
// dispatches to — the two halves a reactor joins. Each consumer decodes its
// event's data into the command's input, which is the service's own reading
// of the payload, and hands the command a claim bound to the event.
func newReactors(
	infra *Infrastructure,
	dom *Domain,
	_ *config.Config,
	lc *lifecycle.Coordinator,
) (*Reactors, error) {
	svc := dom.Command
	rs := &Reactors{Relay: infra.Messaging.Relay(infra.SQL.DB)}
	corelifecycle.Register(lc, "relay", relayStage, rs.Relay)

	var err error
	if rs.Started, err = infra.Messaging.Consume(startedSubscription, svc.Open); err != nil {
		return nil, err
	}
	if rs.Assessed, err = infra.Messaging.Consume(assessedSubscription, svc.Decide); err != nil {
		return nil, err
	}
	if rs.Concluded, err = infra.Messaging.Consume(concludedSubscription, svc.Close); err != nil {
		return nil, err
	}
	corelifecycle.Register(lc, "started", consumeStage, rs.Started)
	corelifecycle.Register(lc, "assessed", consumeStage, rs.Assessed)
	corelifecycle.Register(lc, "concluded", consumeStage, rs.Concluded)
	return rs, nil
}
