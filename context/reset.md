# reset · exercise-design

- **Status:** closeout
- **Session:** plan
- **Branch:** exercise-design

## Disposition

- **Add or sharpen:**
  - `exercise.md` is new: the spike's final demonstration, four services (`exercise`,
    `intelligence`, `command`, `operations`) that play a two-faction exercise of sector dominance.
    It states:
    - the architect's guidelines
    - the world, and the rules that resolve each round
    - each service's commands, events, and reactions
    - the chain one round sets off
    - the evidence each part carries
    - where the services live
    - its assumptions
  - `README.md`: the capability entry names the exercise services, and the path becomes four steps
    (exercise, operations, intelligence, command with the final validation). Evidence 7 is
    restated: an event is emitted only in the transaction of the command that makes it true.
    `exercise.md` joins the Notes list.
  - `design.md` gains a decision: the stream's retention is bounded by a `MaxAge` longer than an
    exercise, and a consumer that falls behind it recovers through a republish on the producer.
    Rejected: an unbounded stream, and a stream that keeps only the last value for each entity.
    The open questions on reactor placement, broker registration, and the import check now name
    the exercise services. "Outbox sequencing" notes that the blob case waits on go-storage.
- **Culled:**
  - `README.md`'s single demonstration service, in both the capabilities and the path.
  - `design.md`'s open question on the stream's unbounded retention, settled by the decision above.
- **Validated:**
  - **The walkthrough checkpoint.** One round traced through the notes, from `ResolveRound(r)` to
    `operations.orders.issued(r+1)`, and one outage of operations and its return. Every rule the
    trace used is in the changed notes.
  - **The editor pass** (`04e2ee9`). It also named intelligence's limits as the suppression rules
    that the README cites, and stated that `RecordOrders` refuses a past round's orders with
    `event.Permanent`.

## Next-focus

A `start` session for step 1 of the path: the **exercise** service (`context/exercise.md`).

- **Scaffold.** Generate `services/exercise` from go-web-sdk-template `template/v0.3.0`, as a
  module in `go.work` with a database of its own on the compose Postgres. At SETTLE, decide
  whether persistence goes through go-database or through sqlate directly.
- **Composition root.** Wire the outbox, the relay, and the nats broker, registered at the lowest
  stage. This root is the pattern the other three services follow.
- **The world.** Build the model: sectors, levels, obstacles, objectives, gates, factions, and
  elements. `ResolveRound` runs on `reactor.Every` in one transaction: move, engage, capture,
  observe, and judge.
- **The API.** `Create`, `Start`, `Pause`, `Resume`, and `Stop`, plus the umpire's view and the
  round history.
- **Events.** It emits `exercise.started`, `exercise.round.observed`, and `exercise.concluded`.
  `RecordOrders` consumes `operations.orders.issued`.
- **Checkpoint.** An exercise runs to its round limit while both factions stand idle, and ends as
  a draw.
