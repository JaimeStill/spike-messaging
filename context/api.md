# The proposed event and reactor API

This is the spike's starting API, not a design to implement verbatim. It began as
`context/messaging-api.md` in standards-lab/org, and from here the spike's sessions own the final
names, signatures, and package homes. `design.md` states what the layer is and why. This document
states what it would expose. Each package is labeled with its intended home, and that home is the
spike's hypothesis.

Every package is built, and its package documentation states its API: `go doc` on `./core/event`,
`./core/reactor`, `./messaging`, `./messaging/outbox`, and `./messaging/memory`. Two are modules
of their own: the outbox's Postgres engine (`go doc ./messaging/outbox/postgres`) and the
JetStream provider (`go doc ./messaging/nats`). The conformance suite is
`messaging/messagingtest`, and both providers pass it. What remains is how a service composes
them.

## How a service composes it

- The composition root connects to NATS and builds the broker once,
  `broker, err := nats.New(ctx, nc, nats.Config{Stream: s, Prefix: p})`, which provisions the
  stream. It registers the broker at the lowest stage, so its `Shutdown` drains the connection
  after every reactor, and reaches the native handle, `broker.Conn()`, only for request and reply.
  `courier/internal/app/infrastructure.go` shows the construction and the request exchange.

- The composition root builds the outbox once, `ob, err := outbox.New(postgres.Engine())`, and
  injects `ob.Emitter()` into the domain. It declares `postgres.Migrations()` ahead of its own set,
  and its verify stage calls `postgres.Verify`.
- A domain's `messaging.go` translation file calls `Emit` inside the store's transaction, passing
  it as the `event.Tx`. It calls it in the transaction that makes the reported state true
  (`design.md`, "Outbox sequencing").
- The composition root runs the relay as a reactor that produces work, at the root stage:
  `reactor.New(ob.Relay(db), broker.Publish, reactor.Grace(d))`. A reactor that consumes sits
  below it, so the drain stops publishing first. A handler that must not act twice claims the
  event with `ob.Claim` in its own transaction. `courier/internal/app/infrastructure.go` and
  `courier/scenario/outbox.go` show the wiring.
- `internal/app/reactors.go` builds each reactor from a subscription and an adapter over a domain
  service method. It subscribes (`src, err := infra.Broker.Subscribe(sub)`), then builds the
  reactor with `reactor.New(src, adapt(dom.X.Method), reactor.Grace(d))`. It registers the reactor
  on the coordinator at the stage it chooses and passes its `Err` to `Monitor`.
  `courier/scenario/coordinator.go` shows the registration.
- The adapter decodes the event's data into the domain's command. The event stops at the process
  boundary, so the domain service sees a command, never an event.
