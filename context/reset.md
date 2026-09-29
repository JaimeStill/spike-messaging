# reset · intelligence

- **Status:** closeout
- **Session:** start
- **Branch:** intelligence

## Disposition

- **Add or sharpen:**
  - `exercise.md`:
    - intelligence is built. The note records where each suppression rule is enforced, K as
      `contact_rounds`, the payload, and the concluded-round rule.
    - New notes for command, from intelligence's assessments:
      - `known` before `holder`;
      - stale objective beliefs;
      - destroyed contacts lingering;
      - `kind` and `strength`;
      - a conclusion arriving before its final round's events.
    - The `directives` stand-in decides once, on round 0.
    - Unbounded redelivery now names intelligence.
    - Tasks are `<category>-<action>`.
  - `design.md`:
    - "Readiness lags `Start`" notes that the lifecycle tests await readiness.
    - A new open question: a durable consumer outlives a process that dies without its release.
  - `README.md`:
    - The capabilities list gains intelligence, courier's `directives` and `assessments`
      stand-ins, and the theater with its check.
    - The path splits the last step into **command**, then **the final validation**.
  - `CLAUDE.md`: the modules paragraph names `services/intelligence`, and the tasks are
    `<category>-<action>`.
- **Integrated:** intelligence's API, stages, events, and configuration are stated by
  `services/intelligence/README.md` and its package documentation (`go doc` on
  `./services/intelligence/domain/intelligence` and `.../fusion`).
- **Validated:**
  - **Checkpoint A, intelligence runs.** The architect ran it by hand over exercise, operations,
    and courier's `directives`. Red's assessments saw b1 at the edge of a scout's sight, kept it
    at its last-seen cell aging 1, 2, 3, and dropped it at round 5 (K = 3). The objectives aged
    and refreshed as sight reached them.
    - An earlier by-hand run found `directives` loses silently when started after round 0,
      because its round-0 directive is skipped as stale.
  - **Checkpoint B, the final validation.**
    - The full task set, integration included, passed across every module.
    - A live run showed the seen-empty drop: red dropped b2 at age 1 once its cell was seen
      empty.
    - Adjusts:
      - courier's release deletes only its own run's consumers. A concurrent run's release had
        deleted `assessments`' consumer; the new test fails on the old release with
        `consumer deleted`.
      - All three services' lifecycle tests await readiness.
  - **Re-plan: stages 6 and 7, the theater.**
    - The fixture has three sectors joined by gates, walls, five objectives, and six elements a
      side. It is validated against exercise's rules and checked by BFS for reachability.
    - `demo-theater`, `assessments --summary`, and the task rename.
    - A 50-round run: losses match on both sides, every rule fires, and red ends with a stale
      belief about `east:9,4`.
    - Adjust: the summary now narrates by round, and `demo-theater-check` reconciles a run
      against exercise's history. On the architect's run: 102 assessments, 0 inconsistencies.
      The check exits 1 when K is set wrong.
  - **Branch review** (reviewer on Opus, all nine findings verified).
    - Fixed:
      - The final round's assessment could be lost when `Close` won the race with the final
        `Observe`. `Close` now records `closed_round` (migration 0002). The new test fails on
        the old rule.
      - The check requires one assessment per round per faction.
      - The round book begins at round 0.
      - The full narration waits for every named faction, with a fallback when the start has
        aged out.
    - Recorded: the courier consumer leak, the unbounded redelivery, and the notes for command.
    - The architect's rerun: 102 assessments, consistent, with `closed_round` 50 on both rows.
  - **The editor pass** (Sonnet): 8 files, prose only. "payload" was reverted to "event entity".

## Next-focus

A `start` session for the path's step 1: **command** (`context/exercise.md`).

- Generate `services/command` the way intelligence was: gonew from go-web-sdk-template
  `template/v0.9.0` (module path `.../go-web-sdk-template/template`), taking the service layer
  renamed, on the `command` database, port 8083, the shared stream.
- It keys its rows by exercise and faction. Its commands are `Open`, `Decide`, and `Close`.
- It consumes `exercise.started`, `intelligence.assessment.issued`, and `exercise.concluded`,
  with the `ErrNotOpen` redelivery and the concluded-round rule intelligence uses.
- It decides by exercise.md's three rules: engage a weaker known contact within 3 cells, forces
  only; otherwise secure the nearest objective not held and not already targeted; otherwise
  hold. It reads `known` before `holder`.
- It issues `command.directive.issued` in the shape operations reads, resending the faction's
  full target state whenever a target changes.
- **Checkpoint.** `demo-theater` runs with command in place of courier's `directives`: elements
  are re-decided every round, after losses and captures, and `demo-theater-check` still reports
  consistent. Settle at SETTLE how the theater tells command's directives apart in its
  narration.
