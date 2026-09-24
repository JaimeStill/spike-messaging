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

- **One adapter serves every reactor.** `scenario/coordinator.go` adapts a component with
  `Start`, `Shutdown`, `Ready`, and `Err` into a `lifecycle.Service` and monitors its `Err`. That
  interface is the proposed component interface, with `Err` as its fourth member.
- **Stages order the drain.** In `group`, the publisher sits at `StageRoot` and the workers at
  stage 0, so the drain stops publishing before the workers drain. A reactor that consumes sits
  below the root, and one that produces work sits at the root, as the HTTP server does.

## Open questions

- Whether go-core's `lifecycle` should gain the component interface ("Lifecycle registration").
  courier's `group` puts consuming reactors at a numbered stage below a producing one at
  `StageRoot`. The demonstration service decides where a reactor sits beside the HTTP server.
- `event.Tx` names the transaction an event is written in, but a pool satisfies it too. The
  outbox step decides whether the outbox can enforce the transaction.
- `event.Encode` writes header values as given, and `Decode` doesn't detect structured mode. The
  provider steps decide where percent-encoding and structured mode belong.
- Where the outbox writer lives, whether the relay polls or listens for notifications, and
  whether a consumer-side inbox table backs idempotency.
- Who provisions a stream, and how readiness and drain run through the coordinator.
- How the nats provider meets the contract where JetStream differs from memory:
  - JetStream accepts an ack that arrives after `AckWait` if no redelivery has happened yet,
    so the provider must drop an outcome once the handler's deadline passes.
  - A member claims one delivery at a time, so the provider fetches one message per pull.
  - `Subscription.Types` entries need a subject-token rule.
- Deduplication on the event `id` joins the conformance suite with the nats provider.
- The import check in the final validation must let a `_test.go` file import the memory
  provider, its test double.
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
