# The proposed event and reactor API

This is the spike's starting API, not a design to implement verbatim. It began as
`context/messaging-api.md` in standards-lab/org, and from here the spike's sessions own the final
names, signatures, and package homes. `design.md` states what the layer is and why. This document
states what it would expose. Each package is labeled with its intended home, and that home is the
spike's hypothesis.

## `reactor` (intended home: go-core)

A reactor joins one source of occurrences to one function and runs for the process lifetime. The
package knows nothing about messaging, and nothing about the coordinator either. A reactor is a
lifecycle component: it has the same `Start`, `Shutdown`, and `Ready` methods that go-storage's
`Store` and go-database's pool already expose. Its stage belongs to the composition root, which
knows the process's dependency order (`design.md`, "Lifecycle registration").

```go
type Func[T any] func(ctx context.Context, occ T) error

type Source[T any] interface {
	// Receive delivers each occurrence to fn until ctx ends, then drains.
	Receive(ctx context.Context, fn Func[T]) error
	Ready() bool
}

// Reactor runs src into fn. Start launches Receive, Shutdown cancels it
// and waits for in-flight handling, and Ready reports src's readiness.
type Reactor[T any] struct{ /* … */ }

func New[T any](src Source[T], fn Func[T]) *Reactor[T]
func (r *Reactor[T]) Start(ctx context.Context) error
func (r *Reactor[T]) Shutdown(ctx context.Context) error
func (r *Reactor[T]) Ready() bool

// Every is the interval source. Its presence proves the contract is not
// shaped by messaging.
func Every(d time.Duration) Source[time.Time]
```

Against today's go-core, the composition root adapts a reactor by hand:

```go
lc.Add(lifecycle.Service{Name: "grants", Stage: stage, Start: r.Start, Shutdown: r.Shutdown, Check: r})
```

## `event` (intended home: go-core)

The CloudEvents 1.0 type and what a domain depends on to emit one. The package uses the standard
library alone.

```go
type Event struct {
	ID, Source, Type, Subject, DataContentType string
	Time       time.Time
	Data       []byte
	Extensions map[string]string // traceparent, among others
}

// Execer is the transaction shape an emitter writes through; sqlate's
// Session satisfies it.
type Execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// Emitter is the one dependency a domain's messaging.go translation file
// takes. The composition root injects the outbox implementation.
type Emitter interface {
	Emit(ctx context.Context, tx Execer, e Event) error
}

// Permanent marks a handler error that must never be redelivered.
func Permanent(err error) error
```

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

- A domain's `messaging.go` translation file calls `Emit` inside the store's transaction. It
  calls it in the transaction that makes the reported state true (`design.md`, "Outbox
  sequencing").
- `internal/app/reactors.go` builds each reactor from a subscription and an adapter over a domain
  service method, `reactor.New(infra.Broker.Subscribe(sub), adapt(dom.X.Method))`, and registers
  it on the coordinator at the stage it chooses.
- The adapter decodes the event's data into the domain's command. The event stops at the process
  boundary, so the domain service sees a command, never an event.
