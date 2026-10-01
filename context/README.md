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

Each package sits in a directory named for its intended home. A committed `go.work` layers the
modules: the root module holds the libraries, `messaging/postgres` is the outbox's and the inbox's
Postgres engine, `messaging/nats` is the JetStream provider, `courier` is the CLI, and each
exercise service under `services/` is a module of its own.
`mise run split-check` holds the root to the standard library, sqlate's engine-agnostic packages,
and go-core, `courier/scenario` to no NATS package, each service's domain to `core` and its own
module with no provider, and each service apart from the others.

- **`core/event`**: the CloudEvents 1.0 type, its codec, `Permanent`, and the vocabulary a domain
  raises its events in: `Define` and `EventKind`, `Queue`, and the `Recorder` that emits them
  through a `Sink` in the command's transaction.
- **`core/reactor`**: the source contract, the interval source, and the grace a reactor drains
  under.
- **`core/lifecycle`** and **`core/logging`**: one-call registration of a component on go-core's
  coordinator, and the throttle that logs a repeating failure once per interval.
- **`messaging`**: the standard tier's broker operations, which are publish, subscribe, and delivery
  groups, with a subscription's start position and the rules every provider shares, and the
  `Runtime`, a service's messaging built from its config and drain timeout over an injected broker
  and engine, with its relay and its consuming reactors. The runtime logs the traffic it carries:
  each event the relay publishes, and each delivery a consumer handles, with its outcome.
- **`messaging/outbox` and `messaging/inbox`**: the engine-agnostic sink and relay, and the
  inbox's claim, each over an `Engine` of statements that an engine's module supplies.
  `messaging/postgres` is the Postgres engine: both packages' statements and the `messaging`
  migration set.
- **`messaging/memory` and `messaging/nats`**: the providers. Both pass the conformance suite,
  `messaging/messagingtest`.
- **courier** (`courier/cmd/courier`, its own module): the spike's CLI, in the Elemental CLI
  layout. Its narrated scenarios each show one capability on a broker, `--broker memory` or
  `--broker nats`. Each step's checkpoint adds its scenarios. On the exercise services' stream,
  `directives` stands in for command, `assessments` narrates assessments and directives,
  `theater` narrates an exercise as the demonstration, one block per round by side, and
  `theater-check` reconciles a run's assessments against exercise's record.
- **The exercise** (`exercise.md`): four services, `exercise`, `intelligence`, `command`, and
  `operations`, that play the skirmish, a seeded two-faction exercise over hidden objectives, all
  built. Each round runs as a chain of events and commands across all four, with loss alerts and
  revised assessments ordered by producer counters. `SEED=7 mise run demo-theater` plays and
  narrates it across the running services, and `mise run demo-theater-check` runs courier's
  `theater-check` against it.

## Path

Complete. The final validation closed with the answer below; the served project's `plan` session
takes it from here.

## Notes

- `design.md`: the decisions the spike starts from, the outbox-sequencing rule, and the open
  questions it settles.
- `exercise.md`: the final demonstration, its guidelines, rules, and services.

## The answer

**Yes.** Four services play a 30-round, two-faction exercise on NATS JetStream through the
broker-agnostic contract, and the same contract passes the conformance suite on the in-memory
provider. Each item of evidence, and where it stands:

1. **The conformance suite passes on both providers**: `messagingtest.Run` on memory and, under
   integration, on the compose NATS, including `StartNewSkipsEarlier` and `DurableResumes` under
   both start positions.
2. **A stop between commit and publish loses no event**:
   `TestStopBetweenCommitAndPublishLosesNoEvent` and `TestPublishedButUnmarkedIsRepublished` in
   `messaging/postgres`. On the running services, `validate-outages` kills command and asserts its
   outbox empties after the restart; no kill has yet landed inside the window, which only timing
   reaches.
3. **A redelivered event is handled once**: `TestClaimIsFirstOnce`, `TestClaimAcrossAckWait`, and
   each service's `TestAClaimedRepeatChangesNothing`. The running services report the repeats
   they see, and none has occurred in a run.
4. **Two replicas in one delivery group share the work**: `validate-replicas`, two operations
   replicas splitting the deliveries by round 10.
5. **A shutdown drains in-flight handling through the coordinator**: `validate-replicas` and
   `validate-outages`; every SIGTERM drains in about 110ms, and the surviving replica carries both
   factions.
6. **No provider import outside the composition root and the provider**: `mise run split-check`.
7. **An event is emitted only in its command's transaction**: `Recorder.Emit` over the outbox
   sink. The composite Postgres and blob case waits on go-storage.
8. **The native request and reply stays inside the provider and the root**: courier's `request`
   scenario, with `split-check` holding `courier/scenario` off NATS.

Beyond the list, `validate-outages` stops each service and NATS in turn mid-exercise: each outage
has its own visible effect while the others keep serving, and every run converges with a
consistent `theater-check`.

What go-messaging's API takes from the spike, beyond what `design.md` already records:
`Subscription.Start`, a start position that is binding configuration, and a finite `MaxDeliver`
for inputs that can arrive early, whose cost, dropping input on any longer failure, needs an
error hook or a dead-letter path (`design.md`, open questions).
