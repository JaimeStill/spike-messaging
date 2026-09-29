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
  - a **gate**, linked to a gate in another sector; from a gate's cell, one step traverses the
    link to the linked gate's cell
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

1. **Move.** Every order applies at once. A step enters an orthogonally adjacent cell, or
   traverses a gate, and entering a gate and traversing it are separate steps. An order that
   leaves the grid or enters an obstacle is refused whole, and the element stays; so is one that
   ends where another element of the same faction ends. Elements that pass through each other do
   not meet.
2. **Engage.** In each cell holding both factions' elements, each faction's strength is summed.
   The stronger faction survives, reduced by the weaker's strength, and its losses fall on its
   weakest element first. A tie destroys both.
3. **Capture.** An objective with only one faction's elements on it becomes that faction's.
4. **Observe.** Each faction observes its own elements, and every enemy element within sight of
   any of them, measured as Chebyshev distance within the same sector. An observation carries each
   element's kind and strength.
5. **Judge.** A faction with no elements loses, which outranks holding every objective; otherwise
   a faction that holds every objective wins. At the round limit, the faction holding more
   objectives wins, or the exercise is a draw.

## The services

Every service keys its rows by exercise and faction, serves both factions from one process, and
claims every event it consumes through its inbox, in the transaction of the command that handles
it.

| Service | Commands: API · reactor · interval | Emits | Reacts to |
|---|---|---|---|
| **exercise**, the world | `Create`, `Start`, `Pause`, `Resume`, `Stop` · `RecordOrders` · `ResolveRound` | `exercise.started`, `exercise.round.observed` (per faction per round), `exercise.concluded` | `operations.orders.issued` |
| **intelligence**, what a faction knows | `Open`, `Observe`, `Close` | `intelligence.assessment.issued` (per observed round) | `exercise.started`, `exercise.round.observed`, `exercise.concluded` |
| **command**, deciding | `Open`, `Decide`, `Close` | `command.directive.issued` (only when an element's target changes) | `exercise.started`, `intelligence.assessment.issued`, `exercise.concluded` |
| **operations**, maneuvering | `Open`, `Assign`, `Maneuver`, `Close` | `operations.orders.issued` (per faction per round) | `exercise.started`, `command.directive.issued`, `exercise.round.observed`, `exercise.concluded` |

- **exercise** is the source of truth for conditions, and is built: `services/exercise`. Its
  README states its API, stages, and events, and `domain/exercise/events.go` its event entities,
  the payloads the other services decode into types of their own. It reads orders from
  `operations.orders.issued` in the shape `ordersIssued` in its `internal/app/reactors.go`
  states, which operations confirms or reshapes. `RecordOrders` refuses an order for a resolved
  round or past the round limit with `event.Permanent`, so it is never redelivered.
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
- **operations** steps each element toward its directive's target, and is built:
  `services/operations`. Its README states its API, stages, and events.
  - It plans along a shortest path found by breadth-first search over open cells and gates, up
    to the element's moves per round (`domain/operations/route`). It keeps a faction's elements
    off one another's final cells by exercise's own rule, so one can follow another into the
    cell it leaves, and two can swap.
  - It confirms exercise's `ordersIssued` as its payload:
    `{exercise, faction, round, orders: [{element, steps: [{sector, x, y}]}]}`.
  - A directive is `{exercise, faction, round, directives: [{element, target}]}`, one event per
    faction per decision, where a null target holds the element.
  - A directive on the round operations last acted on re-issues that round's orders when it
    changes the plan, and exercise keeps the last orders it records, so a directive takes effect
    in the round it arrives. A directive on a later round only sets targets.
  - Its four inputs arrive on four subscriptions, so an observation can be handled before the
    start that opens its operation: it fails with `ErrNotOpen`, which is not permanent, and is
    redelivered after 250ms.
  - courier's `directives` scenario stands in for command.
- command and operations each skip an input from an earlier round than the last they acted on.
  command issues a directive only when a target changes, so when operations skips a lagging
  one, that change is lost: command resends its faction's full target state in each directive,
  or operations applies stale targets without re-issuing orders.

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
- **Smoothed in the final validation.** operations surfaced these, and the end-to-end run must
  settle them:
  - A new consumer's durable starts at the beginning of the shared stream, so a service's first
    boot replays every retained exercise: operations issued orders for all of them, which
    exercise refused.
  - Orders issued after an exercise's last round, before its conclusion is handled, are refused.
  - An input for an exercise never opened is redelivered without bound.
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
- **Tasks.** A service's tasks are named `<service>:<action>`, such as `exercise:serve`.
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
