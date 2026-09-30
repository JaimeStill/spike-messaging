# The exercise

The spike's final demonstration: four small services that play a two-faction exercise of sector
dominance. All four are built. Each round of the exercise runs as a chain of events and commands
across all four, so the demonstration exercises fan-out, chains, ordering, redelivery, delivery
groups, drain, and outages under a steady load. The rules are as simple as the flow allows;
exercise's `rules/doc.go` states them in full.

## Guidelines

1. **Each service's schema is its own.** Outside the event layer the services share nothing, not
   even Go types. A consumer decodes an event into a type of its own, and the contract is the
   event type and its payload.
2. **A service's own commands and queries never depend on another service.** The broker is
   infrastructure, like the database: it registers at the lowest stage and is reported in
   `/readyz`.
3. **Commands are the only way to mutate a service's state.** A reactor is a second way to invoke
   a command, beside HTTP. Queries never mutate.
4. **An event reports a committed mutation.** The producer shapes its payload to what the mutation
   means. The payload is an event entity, the outbound counterpart of a command entity, and is
   never shaped to what some consumer would like. The consumer decides its reaction in its
   reactor registration, usually by calling one of its own commands.
5. **One event can set off a chain** of events across the services.
6. **No service's availability or internal function depends on another's.**
7. **The ruleset grows only for the flow.** The demonstration needs decisions that flow between
   the services, so the rules grow as far as that takes and no further: each rule exists because
   a service must learn, decide, or act on something another service reports.

## The world

- **The skirmish** is the demonstration: one 13×13 sector with no obstacles or gates, three to
  five objectives on rows 3–9, and three squads and two scouts a side on opposite baselines, all
  mirrored through the center. `rules.Skirmish(seed, factions)` lays it out from the exercise's
  seed. A create may still give a map and elements of its own, which the tests use.
- **The seed** is drawn when a create gives none, stored, and read only from exercise's API: the
  layout and every fight follow from it, so no faction's event carries it. A seed replays a run
  together with the orders the run recorded; the services' orders depend on timing, so a seed
  alone replays one only as far as they arrive alike. Checkpoint A saw seed 7 replay identically.
- **Objectives are hidden.** `exercise.started` carries the terrain alone (`Map.Terrain()`), and a
  faction learns an objective only when its observation reports it in sight.
- **Time** runs in rounds of 1s, 30 of them in the fixture. An order for a round already resolved
  is dropped, so latency costs a faction tempo, never correctness.

## Elements and resolution

A squad fields four operators, moves one cell, and sees one; a scout is one operator, moves two,
and sees two. Each operator has health from 1 to 100, `strength` is its element's total, and an
element is `ready`, `engaged` in a fight, or `recovering` from a retreat. A round resolves in one
transaction, in this order (exercise's `rules/doc.go` is the full statement):

1. **Move.** Orders apply at once. An engaged element moves only by a one-step `retreat`, and a
   recovering element not at all. Own elements may end on one cell only where a fight stood at
   the round's start, which is reinforcement.
2. **Pursuit.** An enemy that stood in the cell a retreat left, not recovering, and stays with a
   `pursue` order fires once at the retreating element, which fires back. An unpursued retreat
   escapes without a shot, and the retreating element sits out the next round.
3. **Fight.** In each contested cell every living operator fires once, all at once, at 50% for
   20–60 damage on a random living enemy operator. The draws come from a PCG over the seed and the
   round, in sorted order, so a retry or a replica resolves the round alike.
4. **Capture.** An objective changes hands once one faction ends two rounds in a row alone on it;
   a contested or empty round resets the count.
5. **Observe** each faction's own elements and what they see, and **judge**: elimination, then
   holding every objective, then the most objectives at the limit.

## The services

Every service keys its rows by exercise and faction, serves both factions from one process, and
claims every event it consumes through its inbox, in the transaction of the command that handles
it. Each README states its API, stages, and events.

| Service | Emits | Reacts to |
|---|---|---|
| **exercise**, the world | `exercise.started`, `exercise.round.resolved`, `exercise.objective.lost`, `exercise.round.observed`, `exercise.concluded` | `operations.orders.issued` |
| **intelligence**, what a faction knows | `intelligence.assessment.issued` | `exercise.started`, `exercise.round.observed`, `exercise.objective.lost`, `exercise.concluded` |
| **command**, deciding | `command.directive.issued` | `exercise.started`, `intelligence.assessment.issued`, `exercise.concluded` |
| **operations**, maneuvering | `operations.orders.issued` | `exercise.started`, `command.directive.issued`, `exercise.round.observed`, `exercise.concluded` |

- **exercise** is the umpire. `exercise.round.resolved` is its record of a round, both factions'
  elements included, so no faction's service consumes it and each round's history keeps it.
  `exercise.objective.lost` alerts a faction that lost an objective it held, though none of its
  elements sees it.
- **intelligence** fuses observations into an assessment: own elements, contacts with their age
  (dropped after `contact_rounds` unseen, 3, or once the cell is seen empty), the objectives
  discovered, and `explored`, every cell its elements have had in sight. A loss alert sets the
  objective's holder and, when the assessment already covers the round, issues a revised one.
  Each assessment carries a monotonic `revision`.
- **command** decides for each live element, in this order (`decide/doc.go`): **retreat** a scout,
  or a squad below two thirds of the enemy's strength in its cell, to the open neighbor farthest
  from other contacts; **pursue** in a fight its faction at least matches, or **engage** in place;
  **reinforce** a held fight within 4 steps; **engage** a weaker contact within 3; **secure** the
  nearest known objective not held, an element standing on one claiming it first; **search** the
  nearest unexplored cell, scouts first, spread apart; **rescout** the known objective not held
  and seen longest ago; **hold**. It skips an assessment at or below the last revision it
  decided, issues a directive only when some element's rule or target changes, and gives each
  directive a monotonic `sequence`.
- **operations** plans each element's steps toward its target by breadth-first search, orders a
  one-step flagged retreat or a stepless `pursue`, leaves engaged and recovering elements in
  place, and keeps a faction's elements apart except in its fights. It skips a directive at or
  below the last sequence it applied. `directive_round` no longer guards anything.
- **The rule names** (`retreat`, `pursue`, `engage`, `reinforce`, `secure`, `search`, `rescout`,
  `hold`) are a string contract between command, operations, and courier, with no shared type.
- **courier's `theater`** narrates an exercise as one block per round once the round is complete:
  the observer's record (fights, retreats, each faction's captures and losses), then each
  faction's `knows`, `decides`, and `orders`. The observer's view, the seed and the objectives,
  comes from exercise's API, never from the factions' events. **`theater-check`** reads a run's
  assessments from the stream and the truth from exercise's history API, compares each with the
  observer's own observations, and fails on any inconsistency; `demo-theater-check` runs it.

## One round

1. `exercise.round.resolved(r)`, any `exercise.objective.lost(r)`, then
   `exercise.round.observed(r)`, which fans out to intelligence and to operations.
2. intelligence issues `intelligence.assessment.issued(r)`, and a revision of it when a loss
   alert arrives after the observation.
3. command issues `command.directive.issued` when some element's rule or target changes.
4. operations issues `operations.orders.issued(r+1)`, and re-issues it when a directive changes
   the plan for the round it last acted on.
5. exercise records the orders, and `ResolveRound(r+1)` resolves the next round.

`exercise.concluded` ends the chain, and every service closes its elements.

## Evidence

- **Evidence 3:** every reactor claims the events it consumes, so a redelivery changes nothing.
- **Evidence 4 and 5:** operations runs as two replicas in one delivery group. Stopping one
  mid-exercise drains it, and the other carries both factions.
- **Evidence 6:** the import check requires three things:
  - no service module imports another's
  - no domain package imports `messaging`
  - a provider appears only in a composition root
- **Evidence 7, restated:** an event is emitted only in the transaction of the command that makes
  it true. Whether that command writes SQL alone or orchestrates phased SQL and blob writes makes
  no difference to the layer. The blob case waits on go-storage.
- **Evidence 8** stays proven by courier's `request` scenario. The services use no native feature.
- **The principle.** Each outage has its own visible effect while every other service's API keeps
  serving:
  - With operations down, the elements stand still and exercise keeps resolving.
  - With command down, operations keeps pursuing the standing directives.
  - With intelligence down, decisions stop changing.
  - With exercise down, the world pauses.

  On its return, each service skips its stale backlog, and the exercise converges.
- **Smoothed in the final validation.** operations surfaced these, and the end-to-end run must
  settle them:
  - A new consumer's durable starts at the beginning of the shared stream, so a service's first
    boot replays every retained exercise: operations issued orders for all of them, which
    exercise refused.
  - Orders issued after an exercise's last round, before its conclusion is handled, are refused.
  - An input for an exercise never opened is redelivered without bound, in operations,
    intelligence, and command. The runtime's traffic log now shows each retry.
  - Every broker on the stream sets its configuration, so each service, and courier's
    `--max-age`, must set `max_age` alike.

## Where the services live

- **Modules.** The services are four modules in `go.work`: `services/exercise`,
  `services/intelligence`, `services/command`, and `services/operations`.
- **Scaffold.** Each is generated from go-web-sdk-template `template/v0.9.0` and bumped to the
  SDK versions go-web-service uses.
- **Persistence.** Each persists through go-database and sqlate, in a database of its own on the
  compose Postgres. The environment provisions the empty database (`compose/postgres/init.sql`),
  and the service owns everything in it: go-database's schema service migrates the messaging set
  and the service's own at startup.
- **Tasks.** The mise tasks are named `<category>-<action>`, such as `exercise-serve`,
  `demo-theater`, and `demo-theater-check`. `demo-theater` plays `fixtures/skirmish.json`, or the
  fixture `FIXTURE` names, with the seed `SEED` gives, and narrates it through courier's `theater`
  scenario.
- **Pacing.** Each service's `config.local.json` polls the outbox every 50ms, so a round's four
  relay hops fit in the skirmish's 1s round. `config.json` keeps the 250ms default.
- **Wiring.** Each composition root builds its broker with `nats.New` and its messaging with
  `messaging.New`, and registers the broker, the relay (`Runtime.Relay`), and each consumer
  (`Runtime.Consume`) through `core/lifecycle.Register`. A service's configuration carries a
  `messaging` block and a `nats` block. operations was generated the way exercise was and took
  exercise's service layer, renamed.

## Assumptions

- Two factions are enough to exercise every feature the layer needs.
- A round interval is long enough for the chain's relay hops: four hops between one round's
  observation and the next round's orders.
- Rows keyed by exercise and faction isolate concurrent exercises well enough without a process
  for each one.

## Deferred

Ideas the skirmish's design set aside: operations reporting to intelligence, which would close
the chain into a cycle; drones that intelligence tasks; real time instead of rounds.
