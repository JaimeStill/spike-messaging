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

## Open questions

- Adopt `github.com/cloudevents/sdk-go/v2/event`, or write a spec-conformant type. Weigh the
  existing solution first, and record the loser as a rejected alternative.
- Whether `reactor` is its own package or part of go-core's `lifecycle`.
- Where the outbox writer lives, whether the relay polls or listens for notifications, and
  whether a consumer-side inbox table backs idempotency.
- Who provisions a stream, and how readiness and drain run through the coordinator.
- How trace context propagates as the CloudEvents `traceparent` extension alongside
  go-observability.

## Assumptions

- JetStream's deduplication window covers the relay's retry interval.
- Every event a service emits reports a mutation in its own database.
- The service stays on `database/sql`-shaped sessions (sqlate's `Session`).
