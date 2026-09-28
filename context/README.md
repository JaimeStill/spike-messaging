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

Each package sits in a directory named for its intended home. A committed `go.work` layers four
modules: the root module holds the libraries, `messaging/outbox/postgres` is the outbox's Postgres
engine, `messaging/nats` is the JetStream provider, and `courier` is the application.
`mise run split-check` holds the root to the standard library and sqlate's engine-agnostic
packages, and `courier/scenario` to no NATS package.

- **`core/event`**: the CloudEvents 1.0 type, its codec, the emitter interface a domain depends
  on, and `Permanent`.
- **`core/reactor`**: the source contract, registration on go-core's lifecycle coordinator, and
  the interval source.
- **`messaging`**: the standard tier's broker operations, which are publish, subscribe, and
  delivery groups.
- **`messaging/outbox`**: the engine-agnostic emitter, relay, and inbox, over an `Engine` of
  statements that an engine's module supplies. `messaging/outbox/postgres` is the Postgres engine:
  its statements and the `messaging` migration set.
- **`messaging/memory` and `messaging/nats`**: the providers. Both pass the conformance suite,
  `messaging/messagingtest`.
- **courier** (`courier/cmd/courier`, its own module): the spike's CLI, in the Elemental CLI
  layout. Its narrated scenarios each show one capability on a broker, `--broker memory` or
  `--broker nats`. Each step's checkpoint adds its scenarios.
- **The exercise** (planned, `exercise.md`): four services, `exercise`, `intelligence`, `command`,
  and `operations`, that play a two-faction exercise of sector dominance. Each round runs as a
  chain of events and commands across all four.

## Path

The steps in dependency order. Each is one `start` session, and each session may revise the steps
after it:

1. **exercise**: the scaffold every service follows, and the world: rounds, resolution, its API,
   and its events. It is proven by an exercise that runs to its round limit while both factions
   stand idle.
2. **operations**: directives become orders. courier stands in for command.
3. **intelligence**: observations become assessments, under the suppression rules.
4. **command and the final validation**: the loop closes and runs on its own. The step covers two
   replicas, drain, outages, convergence, and the import check. Its close states the answer.

## Notes

- `design.md`: the decisions the spike starts from, the outbox-sequencing rule, and the open
  questions it settles.
- `api.md`: the starting API.
- `exercise.md`: the final demonstration, its guidelines, rules, and services.

## The final validation

The final step's validation answers the question with this evidence:

1. The conformance suite passes on both providers.
2. A stop between commit and publish loses no event.
3. A redelivered event is handled once.
4. Two replicas in one delivery group share the work.
5. A shutdown drains in-flight handling through the coordinator.
6. An import check finds no provider import outside the composition root and the provider, and
   no `messaging` import in a domain package.
7. An event is emitted only in the transaction of the command that makes it true. The composite
   Postgres and blob case waits on go-storage.
8. The native request-and-reply use stays inside the `nats` provider and the composition root.
