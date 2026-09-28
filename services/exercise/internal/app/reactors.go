package app

import (
	"context"

	"github.com/standards-lab/go-core/lifecycle"

	"github.com/JaimeStill/spike-messaging/core/event"
	"github.com/JaimeStill/spike-messaging/core/reactor"
	"github.com/JaimeStill/spike-messaging/messaging/outbox"
	"github.com/JaimeStill/spike-messaging/services/exercise/internal/config"
)

// Reactors composes the application's event-driven entry points: components
// that watch a source of occurrences and dispatch each one to a call, the
// inbound counterpart to a route. Relay publishes the outbox's committed
// events to the broker.
type Reactors struct {
	Relay *reactor.Reactor[event.Event]
}

// newReactors constructs the reactors and registers each on lc. It takes
// infra for the sources a reactor watches and dom for the domain calls it
// dispatches to — the two halves a reactor joins. A reactor that produces
// work, such as the relay, sits at the root stage beside the server, so
// the drain stops it before the reactors that consume. Each reactor's
// grace, half the shutdown timeout, stays below the coordinator's drain
// deadline, so a handler it cancels is reported before the deadline drops
// the report.
func newReactors(
	infra *Infrastructure,
	_ *Domain,
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

	return &Reactors{Relay: relay}, nil
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
