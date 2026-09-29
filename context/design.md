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
- **A domain raises, and the recorder emits.** A domain owns its events: it declares each with
  `event.Define[T](type)`, an `EventKind` that pairs the type with its event entity `T`, and
  raises them into the `Queue` its command's body receives. `Recorder[Tx].Emit` wraps the body
  into the function a transaction runs, and only when the body succeeds stamps each event (a
  UUIDv7 id, the service's source, the time) and writes it through a `Sink[Tx]` into the same
  transaction. A domain keeps the kinds, the entities, and the raising in its `events.go`, with a
  `command` helper every mutating command runs through. The domain imports only `core/event`.
- **The transaction an event is written in is a type parameter.** `core/event` names no database:
  the composition root fixes `Tx` when it builds the recorder over a sink, `*sqlate.Tx` for the
  outbox's, so a pool fails to compile. Rejected: `event.Tx`, a `database/sql`-shaped interface
  that put the SQL assumption into go-core; carrying the transaction in `ctx`, which hides the
  requirement from the compiler; a command returning its events for a data layer to write, which
  needs a domain to import the outbox; a registry of event kinds, which nothing reads yet; and
  naming the per-command collection `Outbox`, which collides with the durable table.
- **The outbox is engine-agnostic, and an engine is required.** `messaging/outbox` holds no SQL
  and imports no driver. An `outbox.Engine` is the three statements the outbox runs, and
  `outbox.New` checks that each is defined with exactly its parameters. Each engine is a module of
  its own that defines the outbox's and the inbox's statements and ships both tables in one
  migration set; `messaging/postgres` is the first. Rejected: a Postgres-only package, which puts pgx in
  every consumer of go-messaging, as sqlate and go-database avoid with engine sub-modules; and
  standard fallbacks for the native statements, which no second engine here could test.
- **The outbox's SQL is authored statement files run through sqlate's `query` package**, with
  `Verify` for a consumer's verify stage and sqlint in the lint task. Every statement that writes
  declares `transaction: required`: the relay's run on its own `*Tx`, and the sink's and the
  inbox's on the command's.
- **The relay polls, and is a `reactor.Source`.** Its pass is the primary path and its own
  sweeper, and the composition root runs it into a reactor whose handler is the broker's
  `Publish`. Rejected: LISTEN/NOTIFY, which needs a pinned connection outside `database/sql` and
  only lowers latency.
- **The relay drains after the producers, with a last pass.** It sits below the root stage, so the
  drain stops the server and the other producers first, and `outbox.Drain(d)` gives it a last
  pass on a context of its own, bounded by `d` and below its grace, which publishes what they
  committed while they drained. Without it, those events waited for the next start.
- **An inbox table backs idempotency, in `messaging/inbox`.** `Inbox.Claim` inserts the consumer
  and event in the handler's own transaction, and a repeat changes no row. A consumer's domain
  never sees the event: the reactor's adapter hands the command a claim function bound over the
  inbox, which the command runs first in its transaction. The table ships in the engine's one
  migration set beside the outbox's.
- **The spike is a Go workspace.** A committed `go.work` layers the root library module, the
  engine and provider modules, courier, and the exercise services, with no `require` for a
  workspace sibling and no `replace`. In workspace mode a sibling's requirements satisfy the root's imports, so the build
  cannot hold the root's boundary; `mise run split-check` does, as an allow-list of the standard
  library and sqlate's engine-agnostic packages.
- **`messaging/nats` is a module of its own**, as the Postgres engine is, so nats.go stays out of
  the root. Its integration suite runs the conformance cases against the compose stack's NATS, a
  scratch stream per case. Rejected: an embedded nats-server, whose dependencies are heavy and
  which departs from the repository's one test pattern; it returns if a step needs to stop the
  broker under a test.
- **An event type is a sequence of subject tokens.** `event.CheckType` requires `.`-separated
  tokens, none empty, with no whitespace, `*`, or `>`, so a provider can route on the type as a
  subject. `event.Define` checks it when a domain declares a kind, and `messaging.CheckType`
  delegates to it, so a type a domain can define is a type a broker routes; `messaging.IsToken` is the rule for a durable or stream name, which also excludes `/`
  and `\`. Both providers enforce both rules, and the conformance suite proves them. Rejected: a
  rule in the nats provider alone, which lets memory accept what NATS cannot route.
- **Deduplication keys on the event's source and id**, which together identify a CloudEvents
  event and which the inbox already claims on. Both providers keep a two-minute window,
  JetStream's default; nats sends the length-prefixed source and the id as `Nats-Msg-Id`.
  Rejected: the id alone, which silently drops another source's event that reuses an id.
- **The nats broker's constructor provisions its stream** with `CreateOrUpdateStream`, so every
  replica converges on one configuration. The broker owns its connection and is a lifecycle
  component at the lowest stage: its `Shutdown` drains the connection after the reactors, and
  `Ready` reports it. Rejected: a separate provisioning call every root would have to make.
- **A nats source binds its durable at `Receive` and pulls one message at a time.**
  `CreateConsumer` with the configuration derived from the subscription binds or fails on a
  mismatch. The handler's deadline is receipt plus `AckWait`; the consumer's `AckWait` is longer
  by `AckMargin` (250ms), and an outcome that misses the deadline is dropped unsent, because
  JetStream accepts a late ack until it redelivers. An outcome sent on a link slower than the
  margin can land on a redelivery, which costs a duplicate, never a lost event. The margin is
  part of the consumer's configuration, so changing it breaks the bind across a rolling deploy.
  Rejected: the `Messages()` iterator, which prefetches, so one member takes work ahead of the
  others.
- **NATS header values are sent verbatim**, and a value holding CR or LF fails `Publish`. The
  provider neither percent-encodes nor decodes structured mode; a binding that needs encoding,
  such as HTTP, does its own.
- **The native request and reply runs through `nats.Broker.Conn()`**, in the composition root
  alone. `split-check` holds `courier/scenario` to no NATS package.
- **A handler claims on its own context.** `Inbox.Claim` runs in a transaction on the handler's
  context, so the `AckWait` deadline rolls a slow handler's claim back, and the redelivery's claim
  waits on its lock, then claims once (`TestClaimAcrossAckWait`). Under `REPEATABLE READ` or
  `SERIALIZABLE` the waiting claim fails with a serialization error instead, and is redelivered.
- **The stream's retention is bounded, by a `MaxAge` longer than an exercise.** A consumer that
  falls behind the retention recovers through a republish command on the producer, which emits
  its current state again through its own outbox. The republish is built only when a step needs
  it. Rejected: an unbounded stream, which grows without limit, and keeping only the last value
  for each entity, which puts an entity token in the subject and changes the type rule.
- **A service's broker is a lifecycle component at stage 0.** `nats.New` connects and provisions
  the stream, which is I/O, and a service's composition root may do none at construction, so the
  root wraps it in a component that provisions at `Start`, and whose `Subscribe` returns a source
  that binds at `Receive` (`services/exercise/internal/app/broker.go`). The connection reconnects
  without limit, so an outage makes the broker not ready rather than ending the process. This is
  evidence for go-messaging's constructor: it should construct without I/O and provision at a
  start, as go-database's pool does.

## Outbox sequencing

The emitter writes the row in the mutation's transaction. The relay handles each row in a
transaction of its own: it claims the oldest unpublished row with `FOR UPDATE SKIP LOCKED`,
publishes the event while holding the row, and marks it published in the same transaction. A
handler error, a stop, a timeout, or a failed commit leaves the row unpublished, and a later pass
republishes it under the event's source and id (`Nats-Msg-Id` on JetStream), so delivery is at
least once.
The transaction is bounded at twice the relay's handler timeout.

This departs from spike-blobfs's two-step, which publishes and then marks in separate steps.
Holding the claim across the publish lets several relays share the outbox without publishing a
row concurrently. The cost is a row lock and a connection held for the length of the publish.

One relay publishes in `seq` order, which is insertion order among committed rows, not commit
order, and holds the outbox back behind a row that fails. Several relays publish in no particular
order.

Evidence 2 holds: `TestStopBetweenCommitAndPublishLosesNoEvent` in `messaging/postgres`,
and courier's `outbox` scenario on a scratch database. Marking before the publish fails that test.

The rule under test: **an event is enqueued in the transaction that makes the state it reports
true.** For a composite operation, that is the complete step's transaction, never the begin
step's. The spike proves the rule on commands that write SQL alone. The Postgres and blob case
waits on go-storage.

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
  `StageRoot` for the same reason. A reactor that consumes sits below the root, and one that
  produces work sits at the root, as the HTTP server does.
- **Readiness lags again.** `request` waits on its responder's own `Ready` before it sends,
  because the coordinator is ready before its services' sources are receiving.

The evidence from the exercise service (`services/exercise/internal/app`):

- **The stages.** 0: the database and the broker; 1: go-database's schema service; 2: the
  statements' verification; 3: the orders reactor, which consumes; 4: the relay; root: the server
  and the resolver, which produce. Consumers sit below the relay, and the relay below the
  producers.
- **A library constant sets a stage.** The verify stage is `admin.Stage + 1`, go-database's
  constant, and every stage above it follows from it: the smell a registration by stage at the
  call site removes.
- **The adapter recurs.** `reactors.go` has its own `register`, courier's `add` again, with the
  same four methods.

## Open questions

- Whether go-core's `lifecycle` should gain the component interface ("Lifecycle registration").
- The import check in the final validation must let a `_test.go` file import the memory
  provider, its test double, and must hold the exercise services apart from each other.
  `split-check`'s allow-list over `go list -deps -test` is a starting shape for it.
- How the relay and a nats source report the errors they survive. A database error only makes
  the relay not ready, and a handler error, such as a broker that is down, reaches nothing; a
  source's failing pulls only make it not ready. Both need an error hook or the observability
  layer. The exercise service's resolver logs a persisting failure once per interval as a
  stopgap.
- A row that can never be published needs a quarantine or dead-letter mark. A corrupt row ends
  the relay on every start, and a handler that always fails holds the outbox back behind its row.
- Retention: published outbox rows and inbox rows are never purged.
- A stream's configuration is last-writer-wins: every broker on it provisions it with
  `CreateOrUpdateStream`, so the services sharing the exercise's stream must configure it alike,
  and one set to an unbounded `MaxAge` removes the retention. go-messaging needs a way to bind a
  stream without provisioning it, or one owner for its configuration.
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
