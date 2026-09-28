# The exercise

The spike's final demonstration, planned: four small services that play a two-faction exercise of
sector dominance. Each round of the exercise runs as a chain of events and commands across all
four, so the demonstration exercises fan-out, chains, ordering, redelivery, delivery groups, drain,
and outages under a steady load. The rules are deliberately simple. Scale, meaning the elements and
the sector layout, is what changes to make an exercise show the layer well.

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
7. **It stays a spike.** The ruleset is simplified and never grows into game design.

## The world

- An **exercise** has one or more sectors, exactly two factions, a round interval, and a round
  limit.
- A **sector** holds one **level**, a W×H grid. A cell is open, or carries one feature:
  - an **obstacle**, which is impassable
  - an **objective**, held by the last faction to end a round alone on it
  - a **gate**, linked to a gate in another sector; entering it moves the element into the
    linked sector
- The map, meaning the grids and their features, is public. The elements are not.
- Time runs in **rounds**. The exercise service resolves one round per interval, and every order
  for that round applies at once. An order for a round already resolved is dropped, so a late or
  absent faction stands still. Latency costs a faction tempo, never correctness.

## Elements and resolution

| Kind | Strength | Moves per round | Sight |
|------|----------|-----------------|-------|
| force | set per element by the exercise | 1 | 2 |
| scout | 1 | 2 | 4 |

A round resolves in one transaction, in this order:

1. **Move.** Every order applies at once. A move off the grid, into an obstacle, or onto a cell
   where another element of the same faction ends is refused, and the element stays. Elements
   that pass through each other do not meet.
2. **Engage.** In each cell holding both factions' elements, each faction's strength is summed.
   The stronger faction survives, reduced by the weaker's strength, and its losses fall on its
   weakest element first. A tie destroys both.
3. **Capture.** An objective with only one faction's elements on it becomes that faction's.
4. **Observe.** Each faction observes its own elements, and every enemy element within sight of
   any of them, measured as Chebyshev distance within the same sector. An observation carries each
   element's kind and strength.
5. **Judge.** A faction that holds every objective wins, and a faction with no elements loses. At
   the round limit, the faction holding more objectives wins, or the exercise is a draw.

## The services

Every service keys its rows by exercise and faction, serves both factions from one process, and
claims every event it consumes with `Outbox.Claim`, in the transaction of the command that handles
it.

| Service | Commands: API · reactor · interval | Emits | Reacts to |
|---|---|---|---|
| **exercise**, the world | `Create`, `Start`, `Pause`, `Resume`, `Stop` · `RecordOrders` · `ResolveRound` | `exercise.started`, `exercise.round.observed` (per faction per round), `exercise.concluded` | `operations.orders.issued` |
| **intelligence**, what a faction knows | `Open`, `Observe`, `Close` | `intelligence.assessment.issued` (per observed round) | `exercise.started`, `exercise.round.observed`, `exercise.concluded` |
| **command**, deciding | `Open`, `Decide`, `Close` | `command.directive.issued` (only when an element's target changes) | `exercise.started`, `intelligence.assessment.issued`, `exercise.concluded` |
| **operations**, maneuvering | `Open`, `Assign`, `Maneuver`, `Close` | `operations.orders.issued` (per faction per round) | `exercise.started`, `command.directive.issued`, `exercise.round.observed`, `exercise.concluded` |

- **exercise** is the source of truth for conditions. `ResolveRound` runs on its own
  `reactor.Every` at the round interval. `RecordOrders` refuses orders for a past round with
  `event.Permanent`, so they are never redelivered. Its queries serve the umpire's full view and
  the round history.
- **intelligence** fuses its faction's observations into an assessment: its own elements, the
  contacts it knows of with their age, and the status of each objective. Five suppression rules
  limit what it can know:
  - sight radius by kind
  - sight stopping at the sector's edge
  - a contact kept at its last-seen cell and dropped after K rounds unseen
  - nothing that a round did not reveal
  - the chain's own lag
- **command** decides, for each live element on each assessment:
  1. **Engage** a weaker known contact within 3 cells. Forces only.
  2. Otherwise, **secure** the nearest objective that its faction does not hold and that none of
     its other elements is already heading for.
  3. Otherwise, **hold**.
- **operations** steps each element toward its directive's target, along a shortest path found
  by breadth-first search over open cells and gates, up to the element's moves per round.
- command and operations each skip an input from an earlier round than the last they acted on.

## One round

1. `exercise.round.observed(r)` fans out to intelligence and to operations.
2. intelligence issues `intelligence.assessment.issued(r)`.
3. command issues `command.directive.issued` for every element whose target changed.
4. operations issues `operations.orders.issued(r+1)`.
5. exercise records the orders, and `ResolveRound(r+1)` emits `exercise.round.observed(r+1)`.

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

## Where the services live

- **Modules.** The services are four modules in `go.work`: `services/exercise`,
  `services/intelligence`, `services/command`, and `services/operations`.
- **Scaffold.** Each is generated from go-web-sdk-template (`template/v0.3.0`).
- **Persistence.** Each persists through sqlate and the outbox's Postgres engine, in a database of
  its own on the compose Postgres.
- **Wiring.** Each composition root wires its broker, outbox, relay, and reactors itself. The
  repetition across four roots is evidence for what go-messaging should package.

## Assumptions

- Two factions are enough to exercise every feature the layer needs.
- A round interval is long enough for the chain's relay hops: four hops between one round's
  observation and the next round's orders.
- Rows keyed by exercise and faction isolate concurrent exercises well enough without a process
  for each one.
