# spike-messaging

A spike that builds the proposed event and reactor layer and runs it on NATS JetStream and on an
in-memory provider. What it finds is evidence for the served project, standards-lab/org
(`goals.v1.messaging` in its `context/roadmap.toml`). The decision is made there. The spike
decides nothing on its own.

## The question

Can one broker-agnostic event and reactor contract, built on CloudEvents with outbox emission,
run a service's reactors on NATS JetStream and on an in-memory provider, with no broker import
outside the composition root and the provider?

The answer decides go-messaging's module API, which primitives go-core gains, and how
`v1.messaging` breaks into tasks.

## Capabilities

Each package sits in a directory named for its intended home:

- **`core/event`**: the CloudEvents 1.0 type, its codec, the emitter interface a domain depends
  on, and `Permanent`.
- **`core/reactor`**: the source contract, registration on go-core's lifecycle coordinator, and
  the interval source.
- **`messaging`**: the standard tier's broker operations, which are publish, subscribe, and
  delivery groups.
- **`messaging/outbox`**: the emitter over an outbox table, the relay, and the table shipped as a
  migration set.
- **`messaging/memory` and `messaging/nats`**: the providers, and the conformance suite they both
  pass.
- **The demonstration service**: a composition root, a domain that emits, a reactor, and the
  import check.

## Path

The steps in dependency order. Each is one `start` session, and each session may revise the steps
after it:

1. **`messaging`, `messaging/memory`, and the conformance suite**: publish, subscribe, delivery
   groups, and acknowledge, redeliver, and terminate. Proves 1 on memory, and 3.
2. **`messaging/outbox` on Postgres**: the emitter, the relay, the migration set, and a compose
   stack. Proves 2.
3. **`messaging/nats`**: conformance on JetStream, deduplication, stream provisioning, and the
   native request and reply. Proves 1 on NATS, and 8.
4. **The demonstration service**: two replicas, drain, the import check, and a composite
   two-step operation. Proves 4 through 7. Its validation is the final validation, and its close
   states the answer.

## Notes

- `design.md`: the decisions the spike starts from, the outbox-sequencing rule, and the open
  questions it settles.
- `api.md`: the starting API.

## The final validation

The final step's validation answers the question with this evidence:

1. The conformance suite passes on both providers.
2. A stop between commit and publish loses no event.
3. A redelivered event is handled once.
4. Two replicas in one delivery group share the work.
5. A shutdown drains in-flight handling through the coordinator.
6. An import check finds no provider import outside the composition root and the provider, and
   no `messaging` import in a domain package.
7. A composite Postgres and blob operation emits its event only from its complete step's
   transaction.
8. The native request-and-reply use stays inside the `nats` provider and the composition root.
