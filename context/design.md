# Design

The decisions the spike starts from. They come from `context/messaging.md` in standards-lab/org,
which also holds the capability ledger and the rejected alternatives. From
here, this note is the spike's own.

## Decisions

- **The envelope is CloudEvents 1.0**, in binary mode, with its attributes carried as message
  headers under the CloudEvents NATS protocol binding.
- **The envelope type is the spike's own**, in `core/event`, on the standard library alone.
  Rejected: `github.com/cloudevents/sdk-go/v2/event`. Its event package imports json-iterator,
  and sdk-go is one module, so importing it brings zap, testify, and x/time into the module
  graph. go-core depends on nothing outside the standard library.
- **`reactor` is its own package**, not part of `lifecycle`. It imports neither `lifecycle` nor
  `messaging`, and the composition root registers it.
- **Emission goes through a transactional outbox.** The event row is written in the mutation's
  own transaction, and a relay publishes it afterward. Delivery is at least once. A reactor is
  idempotent, keyed on the event's `id`.
- **The providers are NATS JetStream and an in-memory provider.** The in-memory provider serves
  as both the conformance double and the test double.
- **The reactor contract is source-agnostic.** A reactor runs one source of occurrences on the
  lifecycle coordinator. A subscription, an interval, and a schedule are all sources.
- **The source decides what a handler error means.** A subscription redelivers and keeps
  receiving, and `event.Permanent` terminates. `Every` ends, because an interval has nothing to
  redeliver. A handler keeps a deadline the source sets for each occurrence, such as `AckWait`,
  while the reactor still detaches it from the source's cancellation.
- **A subscription's Name is both the durable consumer and its delivery group.** Members under
  one Name share its position and split its work, which maps onto a JetStream durable pull
  consumer. Rejected: a separate `Group` field. JetStream has no second level of grouping
  within a durable, so memory would have had to invent one. `go doc ./messaging` states the
  delivery rules that follow.
- **The scope is the standard tier plus one native use**: request and reply through the `nats`
  provider's handle.
- **The transaction an event is written in is enforced at compile time.** `event.Tx` requires
  `Commit` and `Rollback` beside a `database/sql` session's methods, so a pool is not one, and
  `*sql.Tx` and sqlate's `*Tx` are. Rejected: sqlate's runtime assertion on its own `*Tx`, which
  refuses a `*sql.Tx`, and a documented rule nothing enforces.
- **The outbox is engine-agnostic, and an engine is required.** `messaging/outbox` holds no SQL
  and imports no driver. An `outbox.Engine` is the four statements the outbox runs, and
  `outbox.New` checks that each is defined with exactly its parameters and that the two a caller
  runs on its own `event.Tx` do not declare a transaction required. Each engine is a module of its
  own that defines every statement and ships the tables as a migration set;
  `messaging/outbox/postgres` is the first. Rejected: a Postgres-only package, which puts pgx in
  every consumer of go-messaging, as sqlate and go-database avoid with engine sub-modules; and
  standard fallbacks for the native statements, which no second engine here could test.
- **The outbox's SQL is authored statement files run through sqlate's `query` package**, with
  `Verify` for a consumer's verify stage and sqlint in the lint task. `transaction: required`
  marks only the statements the relay runs on sqlate's `*Tx`.
- **The relay polls, and is a `reactor.Source`.** Its pass is the primary path and its own
  sweeper, and the composition root runs it into a reactor whose handler is the broker's
  `Publish`. Rejected: LISTEN/NOTIFY, which needs a pinned connection outside `database/sql` and
  only lowers latency.
- **An inbox table backs idempotency.** `Outbox.Claim` inserts the consumer and event in the
  handler's own transaction, and a repeat changes no row. The table is in the outbox's migration
  set, so both shipped in one released migration.
- **The spike is a Go workspace.** A committed `go.work` layers the root library module, the
  Postgres engine module, and courier, with no `require` for a workspace sibling and no
  `replace`. In workspace mode a sibling's requirements satisfy the root's imports, so the build
  cannot hold the root's boundary; `mise run split-check` does, as an allow-list of the standard
  library and sqlate's engine-agnostic packages.

## Outbox sequencing

The emitter writes the row in the mutation's transaction. The relay handles each row in a
transaction of its own: it claims the oldest unpublished row with `FOR UPDATE SKIP LOCKED`,
publishes the event while holding the row, and marks it published in the same transaction. A
handler error, a stop, a timeout, or a failed commit leaves the row unpublished, and a later pass
republishes it under the event `id` (`Nats-Msg-Id` on JetStream), so delivery is at least once.
The transaction is bounded at twice the relay's handler timeout.

This departs from spike-blobfs's two-step, which publishes and then marks in separate steps.
Holding the claim across the publish lets several relays share the outbox without publishing a
row concurrently. The cost is a row lock and a connection held for the length of the publish.

One relay publishes in `seq` order, which is insertion order among committed rows, not commit
order, and holds the outbox back behind a row that fails. Several relays publish in no particular
order.

Evidence 2 holds: `TestStopBetweenCommitAndPublishLosesNoEvent` in `messaging/outbox/postgres`,
and courier's `outbox` scenario on a scratch database. Marking before the publish fails that test.

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

The evidence from step 1's `cmd/every`, now courier's `every` scenario:

- **One adapter.** It is hand-written, at `StageRoot`.
- **More than three methods.** The reactor also needs `Err`, which the root passes to `Monitor`.
  Registering a reactor takes two calls (`Add` and `Monitor`) and a rule tying `reactor.Grace`
  to the coordinator's drain timeout. A single registration could carry all of it.
- **Readiness lags `Start`.** The coordinator marks the process ready as soon as `Start`
  returns, before the reactor's source is receiving, so a probe of the component's check can
  briefly read not ready.
- **The coordinator drops errors in two windows.** It stops reading monitored channels at the
  signal, and it drops a drain error that arrives at its deadline. The reactor covers both
  itself: `Shutdown` returns a failure that was sent on `Err` but never read, and `Grace`, set
  below the drain timeout, reports cancelled handlers before the deadline. A coordinator that
  gave participants a short window to report late errors would make both unnecessary.

The evidence from courier's scenarios:

- **One adapter serves every reactor.** `courier/scenario/coordinator.go` adapts a component with
  `Start`, `Shutdown`, `Ready`, and `Err` into a `lifecycle.Service` and monitors its `Err`. That
  interface is the proposed component interface, with `Err` as its fourth member.
- **Stages order the drain.** In `group`, the publisher sits at `StageRoot` and the workers at
  stage 0, so the drain stops publishing before the workers drain. In `outbox`, the relay sits at
  `StageRoot` for the same reason. A reactor that consumes sits
  below the root, and one that produces work sits at the root, as the HTTP server does.

## Open questions

- Whether go-core's `lifecycle` should gain the component interface ("Lifecycle registration").
  courier's `group` puts consuming reactors at a numbered stage below a producing one at
  `StageRoot`. The demonstration service decides where a reactor sits beside the HTTP server.
- `event.Encode` writes header values as given, and `Decode` doesn't detect structured mode. The
  provider steps decide where percent-encoding and structured mode belong.
- Who provisions a stream, and how readiness and drain run through the coordinator.
- How the nats provider meets the contract where JetStream differs from memory:
  - JetStream accepts an ack that arrives after `AckWait` if no redelivery has happened yet,
    so the provider must drop an outcome once the handler's deadline passes.
  - A member claims one delivery at a time, so the provider fetches one message per pull.
  - `Subscription.Types` entries need a subject-token rule.
- Deduplication on the event `id` joins the conformance suite with the nats provider.
- The import check in the final validation must let a `_test.go` file import the memory
  provider, its test double. `split-check`'s allow-list over `go list -deps -test` is a starting
  shape for it.
- How the relay reports the errors it survives. A database error only makes it not ready, and a
  handler error, such as a broker that is down, reaches nothing. It needs an error hook or the
  observability layer.
- A row that can never be published needs a quarantine or dead-letter mark. A corrupt row ends
  the relay on every start, and a handler that always fails holds the outbox back behind its row.
- Retention: published outbox rows and inbox rows are never purged.
- The nats step: a redelivery that arrives while a handler still runs makes the second
  `Outbox.Claim` wait on the first handler's transaction, which interacts with `AckWait`. Under
  `REPEATABLE READ` or `SERIALIZABLE`, it fails with a serialization error instead.
- Whether `Outbox.Claim` moves to a sibling inbox package. It sits in `messaging/outbox` because
  its table is in the same migration set.
- The relay's 10s default `Timeout` is untested against a slow broker.
- At promotion, the engine module and any other sibling module need real `require` lines. They
  build today only through `go.work`.
- How trace context propagates as the CloudEvents `traceparent` extension alongside
  go-observability.

## The CLI layout

courier follows the Elemental CLI layout (standards-lab/org's `context/cli-applications.md`), and
the layout fits a spike whose programs are lifecycle runs. It differs from clutch in three ways:

- A scenario can reject an invalid combination of flags before its first step, through a
  `Validate` hook.
- `scenario.ErrUsage` marks a usage error, and `App.Run` maps it to `process.ExitUsage`.
- The `scenario` mount needs a `RunE` of its own. cobra skips the `Args` check on a command
  with no run function, so an unknown scenario would otherwise print help and exit 0. clutch's
  mount has the same gap.

## Assumptions

- JetStream's deduplication window covers the relay's retry interval.
- Every event a service emits reports a mutation in its own database.
- The service stays on `database/sql`-shaped sessions (sqlate's `Session`).
