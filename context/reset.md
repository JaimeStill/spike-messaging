# reset · operations

- **Status:** closeout
- **Session:** start
- **Branch:** operations

## Disposition

- **Add or sharpen:**
  - `design.md`: new decisions:
    - A broker constructs without I/O and starts as a lifecycle component at stage 0
      (`nats.New(cfg)`, `Start`, `ErrNotStarted`, `ErrStarted`). This replaces the stage-0
      wrapper decision and settles the constructor evidence.
    - A service's messaging is one `messaging.Runtime`: `New` over an injected broker and engine,
      `Relay`, generic `Consume[T]`, logged refusals, and the `Claim` alias.
    - A service's configuration has a `messaging` block and a provider's `nats` block.
    - A reactor's grace is `reactor.GraceWithin(drain)`.
  - `design.md`: existing entries sharpened:
    - The nats broker provisions its stream at `Start`.
    - split-check allows go-core.
    - "Lifecycle registration" records `core/lifecycle.Register`, used by every root, and
      operations' rule: the relay sits below every stage that commits events.
    - The error-reporting question names `Consume`'s refusal log and the throttle as stopgaps.
  - `exercise.md`: operations is built:
    - its route rule matches exercise's;
    - the orders and directive payloads;
    - the re-issue rule;
    - `ErrNotOpen` redelivery;
    - command must resend its full target state (review finding 7);
    - the wiring through the shared packages;
    - the four points the final validation must smooth out.
  - `README.md`: the capabilities list gains `core/lifecycle`, `core/logging`, the Runtime, and
    operations; the path drops the finished step.
  - `CLAUDE.md`: the modules paragraph names `services/operations`.
- **Integrated:** the pieces exercise's root repeated are packages now, whose documentation states
  their API (`go doc` on `./core/lifecycle`, `./core/logging`, `./messaging`,
  `./messaging/nats`). `services/operations` (its README and `internal/app`) is the second working
  reference for how a service composes them.
- **Validated:**
  - **Checkpoint A, the shared root.**
    - The full task set passed.
    - `exercise:serve` was ready.
    - courier's `outbox` and `request` scenarios ran on NATS.
    - Adjust: split-check allows all of go-core.
  - **Checkpoint B, operations: the final validation.**
    - The full task set passed across every module.
    - By hand, with both services on the compose stack, courier's `directives` scenario sent red's
      force and scout to the two objectives. The exercise concluded "red wins by objectives" at
      round 3 of 20, the minimum.
    - Both outboxes were fully published, and both services drained cleanly.
  - **Branch review** (reviewer on Opus, findings verified). Every finding was fixed as a
    checkpoint B adjust, and the full task set and the by-hand run passed again:
    - `Assign` re-issues orders only for a directive on the round last acted on; the new test
      fails on the old rule.
    - `Plan` applies exercise's collision rule, so following and swaps now move.
    - A Postgres inbox round trip proves `Consume`'s claim binding, and both domains' `Claim` is
      an alias.
    - Minor items: `GraceWithin` moved to `core/reactor`, the nats `Start` guard, courier's
      `add` removed, and no second dial.
  - **The editor pass** (Sonnet): 13 files, prose only.

## Next-focus

A `start` session for the path's step 1: **intelligence** (`context/exercise.md`).

- Generate `services/intelligence` the way operations was generated: gonew from
  go-web-sdk-template `template/v0.9.0` (module path `.../go-web-sdk-template/template`), taking
  exercise's service layer renamed, on the `intelligence` database, port 8082, the shared stream.
- It keys its rows by exercise and faction. Its commands are `Open`, `Observe`, and `Close`.
- It consumes `exercise.started`, `exercise.round.observed`, and `exercise.concluded` through
  `Runtime.Consume`, with the `ErrNotOpen` redelivery operations uses.
- It fuses each faction's observations into an assessment under the five suppression rules. It
  emits `intelligence.assessment.issued` per observed round: the faction's own elements, the
  contacts it knows with their age, and each objective's status.
- A test or a courier scenario reads the assessments, standing in for command.
- **Checkpoint.** Over a running exercise, the assessments track a faction's contacts, age them
  out after K rounds unseen, and reveal nothing a round did not.
