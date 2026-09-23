# The proposed event and reactor API

This is the spike's starting API, not a design to implement verbatim. It began as
`context/messaging-api.md` in standards-lab/org, and from here the spike's sessions own the final
names, signatures, and package homes. `design.md` states what the layer is and why. This document
states what it would expose. Each package is labeled with its intended home, and that home is the
spike's hypothesis.

`core/event` and `core/reactor` are built, and their package documentation (`go doc ./core/event`,
`go doc ./core/reactor`) states their API. The sections below cover what is not built yet.

## `messaging` (intended home: go-messaging, base module)

The standard tier's broker operations.

```go
type Publisher interface {
	Publish(ctx context.Context, e event.Event) error
}

type Subscription struct {
	Name       string   // the durable identity
	Group      string   // the delivery group: members share the work
	Types      []string // a filter on event type
	MaxDeliver int
	AckWait    time.Duration
}

type Broker interface {
	Publisher
	// Subscribe returns a reactor source. The handler's return is the
	// outcome: nil acknowledges, an error redelivers, and event.Permanent
	// terminates.
	Subscribe(sub Subscription) reactor.Source[event.Event]
}
```

## `messaging/outbox` (intended home: go-messaging)

The emitter implementation over an outbox table, plus the relay that publishes rows outside any
transaction and marks them published. The relay republishes under the event `id` as the
deduplication key. The table ships as a migration set.

```go
func NewEmitter(...) event.Emitter
type Relay struct{ /* publishes unpublished rows; the relay is its own sweeper */ }
```

## Providers

- `messaging/nats`: the JetStream adapter, exposing the native handle that the request-and-reply
  use reaches.
- `messaging/memory`: the in-memory provider that runs the conformance suite.

## How a service composes it

- A domain's `messaging.go` translation file calls `Emit` inside the store's transaction, passing
  it as the `event.Tx`. It calls it in the transaction that makes the reported state true
  (`design.md`, "Outbox sequencing").
- `internal/app/reactors.go` builds each reactor from a subscription and an adapter over a domain
  service method, `reactor.New(infra.Broker.Subscribe(sub), adapt(dom.X.Method), reactor.Grace(d))`,
  registers it on the coordinator at the stage it chooses, and passes its `Err` to `Monitor`
  (`cmd/every` shows the registration).
- The adapter decodes the event's data into the domain's command. The event stops at the process
  boundary, so the domain service sees a command, never an event.
