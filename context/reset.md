# reset · exercise

- **Status:** closeout
- **Session:** start
- **Branch:** exercise

## Disposition

- **Add or sharpen:**
  - `design.md`: new decisions:
    - A domain raises, and the recorder emits: `EventKind`/`Define`, `Queue`, `Recorder[Tx].Emit`, `Sink[Tx]`, and the domain's `events.go`.
    - The transaction an event is written in is a type parameter. This replaces the `event.Tx` decision, with its rejected alternatives.
    - The relay drains after the producers, with `outbox.Drain`.
    - A service's broker is a stage-0 lifecycle component that provisions at `Start`. This is evidence for go-messaging's constructor.
  - `design.md`: existing entries sharpened:
    - The inbox decision names `messaging/inbox`, and the engine and workspace decisions name `messaging/postgres` and the service modules.
    - `event.CheckType` now carries the event-type rule.
    - "Lifecycle registration" gains the exercise service's evidence: its stages, the `admin.Stage` smell, and the adapter recurring.
    - Open questions: the placement and broker-registration questions are settled and removed. The `Claim`-move question is settled by the inbox. New question: a stream's configuration is last-writer-wins. The error-reporting question notes the resolver's throttled log as a stopgap.
  - `exercise.md`: the ruleset decisions:
    - Steps are orthogonal.
    - Entering a gate and traversing it are separate steps.
    - Elimination outranks holding every objective.
    - Orders past the round limit are refused.
  - `exercise.md`: exercise is built, and its README and `events.go` hold its contracts; `ordersIssued` is exercise's reading of operations' payload.
  - `exercise.md`: "Where the services live" states:
    - template v0.9.0;
    - go-database;
    - the databases provisioned by `compose/postgres/init.sql`, with the service owning its schema;
    - the `<service>:<action>` tasks;
    - the repeated root as go-messaging evidence.
  - `README.md`: the capability map names the domain vocabulary, `messaging/inbox`, `messaging/postgres`, the service modules, and exercise as built. The path drops step 1, and its new step 1 extracts the repeated root before operations.
  - `CLAUDE.md`: the modules paragraph names `services/*` and the task convention.
- **Integrated:** deleted `api.md`. Each package's documentation states its API (`go doc` on `./core/event`, `./messaging/inbox`, `./messaging/outbox`, `./messaging/postgres`, `./messaging/nats`). `services/exercise` (its README and `internal/app`) is the working reference for how a service composes them.
- **Validated:**
  - **Checkpoint A, the layer refactored.**
    - The full task set passed.
    - courier's `outbox` scenario ran through a recorder on both brokers.
    - No `Emitter` or `event.Tx` remains.
    - Adjust: event ids use the standard library's `uuid.NewV7`.
  - **Checkpoint B, the root boots.**
    - `/readyz` reports the database, broker, schema, and relay.
    - The stream carries `max_age`.
    - The service drains cleanly.
    - Adjust: the task is renamed `exercise:serve`.
  - **Checkpoint C, the final validation.**
    - The full task set passed across every module.
    - By hand, an idle exercise ran at 1s rounds to round 6 and concluded as a draw by limit, and pause held the clock. The outbox held 1 started, 14 observed, and 1 concluded event, all published.
  - **Branch review** (reviewer on Opus, findings verified). The three findings were fixed as checkpoint C adjusts:
    - Each integration process gets a scratch database. Reproduced with a leftover running exercise; the suite now passes.
    - The status commands read under `FOR UPDATE`. The new race test fails on the old read.
    - The relay sits at stage 4 with a last `Drain` pass. A mid-exercise stop had left 10 of 127 rows pending; now it leaves 0 of 212.
  - **The review's lesser observations**, all resolved:
    - concurrency tests;
    - sqlint over the service's SQL;
    - orders past the limit refused;
    - the resolver's failure log throttled;
    - the unused `reads` block removed;
    - broker unit tests;
    - the test watcher provisioning as the service does;
    - the ruleset and module notes above.
  - **The editor pass** (`5d6a1e4`): the session settled the six passages it left open.

## Next-focus

A `start` session for the path's new step 1: extract the composition root every service repeats, then build **operations** (`context/exercise.md`).

- **Extract first.** `services/exercise/internal/app` holds what every service repeats. At SETTLE, decide each piece's home. Where a piece needs NATS or a service's config, it stays in the service layer or in `messaging/nats`, never in the root. The pieces:
  - the broker component (`broker.go`);
  - the reactor `register` adapter;
  - the relay's wiring with `Drain`;
  - the `failures` throttle;
  - the recorder's construction;
  - the `messaging` config block;
  - the integration harness's scratch database and stream.

  The broker component raises the go-messaging constructor question in `design.md`. Exercise moves onto the extraction with its tests green.
- **operations.** Generate `services/operations` the way exercise was generated, on the `operations` database.
  - It keys its rows by exercise and faction.
  - Its commands are `Open`, `Assign`, `Maneuver`, and `Close`.
  - It reacts to `exercise.started`, `command.directive.issued`, `exercise.round.observed`, and `exercise.concluded`, claiming each through the inbox.
  - It steps each element toward its target by breadth-first search over open cells and gates, as `rules` moves them: orthogonal steps, and a gate traversal as a step of its own.
  - It emits `operations.orders.issued` per faction per round, confirming or reshaping exercise's `ordersIssued`.
  - courier stands in for command with a scenario that issues `command.directive.issued`.
- **Checkpoint.** With courier's directives, elements move and capture an objective, and an exercise ends by objectives rather than by the limit.
