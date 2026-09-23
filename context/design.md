# Design

The decisions the spike starts from. They come from `context/messaging.md` in standards-lab/org,
which also holds the capability ledger and the rejected alternatives. From
here, this note is the spike's own.

## Decisions

- **The envelope is CloudEvents 1.0**, in binary mode, with its attributes carried as message
  headers under the CloudEvents NATS protocol binding.
- **Emission goes through a transactional outbox.** The event row is written in the mutation's
  own transaction, and a relay publishes it afterward. Delivery is at least once. A reactor is
  idempotent, keyed on the event's `id`.
- **The providers are NATS JetStream and an in-memory provider.** The in-memory provider serves
  as both the conformance double and the test double.
- **The reactor contract is source-agnostic.** A reactor runs one source of occurrences on the
  lifecycle coordinator. A subscription, an interval, and a schedule are all sources.
- **The scope is the standard tier plus one native use**: request and reply through the `nats`
  provider's handle.

## Outbox sequencing

The outbox follows spike-blobfs's two-step protocol. The emitter writes the row in the
mutation's transaction. The relay publishes outside any transaction, marks the row published,
and republishes on retry under the event `id` (`Nats-Msg-Id` on JetStream). An unpublished row
stays unpublished until the relay succeeds, and the relay's pass is its own sweeper.

The rule under test: **an event is enqueued in the transaction that makes the state it reports
true.** For a composite operation, that is the complete step's transaction, never the begin
step's.

## Lifecycle registration

Infrastructure already joins the lifecycle through the same three methods, `Start`, `Shutdown`,
and `Ready`, and every composition root copies them into a `lifecycle.Service` by hand. The
reactor is the next thing that needs them. The hypothesis: go-core's `lifecycle` gains a component
interface and registration by name and stage (`Register(name, stage, comp)`), with a `Stage` type.
The stage stays at the call site, because it is the process's dependency order, which a library
can't know. A library constant such as go-database's `admin.Stage` is the smell this removes.

The spike builds against published go-core, so it tests the hypothesis without changing it. The
reactor is a component, and the composition root adapts it with today's `lifecycle.Service`. How
often that adapter recurs, and whether anything wants more than the three methods, is the evidence
for the go-core change.

## Open questions

- Adopt `github.com/cloudevents/sdk-go/v2/event`, or write a spec-conformant type. Weigh the
  existing solution first, and record the loser as a rejected alternative.
- Whether `reactor` is its own package or part of go-core's `lifecycle`.
- Whether go-core's `lifecycle` should gain the component interface ("Lifecycle registration"),
  and which stage a reactor takes. A reactor is an entry point like the HTTP server, so it may
  belong at `StageRoot`, drained before the infrastructure it depends on, or at a stage of its own
  between the domains and the root.
- Where the outbox writer lives, whether the relay polls or listens for notifications, and
  whether a consumer-side inbox table backs idempotency.
- Who provisions a stream, and how readiness and drain run through the coordinator.
- How trace context propagates as the CloudEvents `traceparent` extension alongside
  go-observability.

## Assumptions

- JetStream's deduplication window covers the relay's retry interval.
- Every event a service emits reports a mutation in its own database.
- The service stays on `database/sql`-shaped sessions (sqlate's `Session`).
