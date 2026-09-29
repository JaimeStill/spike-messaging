package app

import (
	"github.com/standards-lab/go-core/lifecycle"

	"github.com/JaimeStill/spike-messaging/core/event"
	corelifecycle "github.com/JaimeStill/spike-messaging/core/lifecycle"
	"github.com/JaimeStill/spike-messaging/core/reactor"
	"github.com/JaimeStill/spike-messaging/services/command/internal/config"
)

// relayStage places the relay above every other reactor and below the root,
// so the drain stops it only after the server, which commits events, and it
// publishes what the server committed while it drained.
const relayStage = verifyStage + 1

// Reactors composes the application's event-driven entry points: components
// that watch a source of occurrences and dispatch each one to a call, the
// inbound counterpart to a route. Relay publishes the outbox's committed
// events to the broker.
type Reactors struct {
	Relay *reactor.Reactor[event.Event]
}

// newReactors constructs the reactors and registers each on lc. It takes
// infra for the sources a reactor watches and dom for the domain calls it
// dispatches to — the two halves a reactor joins.
func newReactors(
	infra *Infrastructure,
	_ *Domain,
	cfg *config.Config,
	lc *lifecycle.Coordinator,
) (*Reactors, error) {
	relay := infra.Messaging.Relay(infra.SQL.DB, cfg.ShutdownTimeout.Duration())
	corelifecycle.Register(lc, "relay", relayStage, relay)
	return &Reactors{Relay: relay}, nil
}
