# reset · cleanup

- **Status:** closeout
- **Session:** start
- **Branch:** cleanup

## Disposition

- **Integrated:**
  - `cleanup.md` is deleted; its inputs are built:
    - courier's payloads, mirrors, keys, and theater split;
    - the leftovers from hiding the objectives;
    - operations' parallel maps and `directive_round`;
    - intelligence's `storedPicture`.
  - The rule names are documented under "Rule names" in command's `decide/doc.go`.
- **Add or sharpen:**
  - `design.md`:
    - `CheckName` replaces `IsToken`, and a new entry holds the rules every provider shares
      (the defaults, `Encode`, `Subscription.Normalize`, the engine check).
    - The `Runtime` entry takes the drain timeout once, validates, and exports only `Recorder`;
      the relay's poll is required.
    - A new entry names the composition root, `internal/app` with `internal/config`, and states
      evidence 6 as `split-check` holds it.
    - Lifecycle registration: the step-by-step evidence is condensed.
      - The result is now `Component` (three methods), `Monitored` (adds `Err`), and `Register`
        for the database, the broker, and the reactors.
      - The trade-off: `Register` finds `Err` by type assertion.
    - Open questions:
      - The import check is settled and removed.
      - The relay's timeout: `Runtime` cannot set it.
      - The change-only pattern notes the round interval as an implicit barrier.
      - courier's inconsistent waits and its unread `sequence` are recorded.
  - `exercise.md`:
    - The replay is the rounds, the final conditions, and a consistent check; event totals vary
      with timing.
    - command secures and rescouts a discovered objective, and the `directive_round` sentence
      is gone.
    - The rule names point to `decide/doc.go`.
    - courier reads the rules block from exercise's API, so it mirrors no rule.
  - `README.md`:
    - `split-check` covers evidence 6, and `messaging` holds the shared rules and takes the drain
      timeout.
    - The path is **the final validation** alone.
- **Validated:**
  - **Baseline.** On main, seed 7 ran twice with identical narration apart from latency and
    IDs.
  - **Checkpoint A** (stages 1–14):
    - The full task set passed: build, vet, test, lint with sqlint, split-check, and
      integration.
    - Seed 7's narration and check matched main's; the check was consistent.
    - The API carried the rules block.
    - No intelligence picture held a `grid` key, and operations had no `directive_round`.
  - **Final** (stages 15–17):
    - `go fix` was a no-op, and the full task set passed.
    - Seeds 7 and 11 each ran twice, consistent:
      - Seed 7 matched main.
      - Seed 11's two runs matched each other.
      - One of five seed-7 runs counted 78 `operations.orders.issued` against 79, with identical
        narration: operations' re-issue race. The architect accepted it.
  - **Branch review** (the reviewer on Opus; the session verified the findings). Eight findings;
    six fixed as checkpoint adjusts, and the full task set passed again:
    - The directives stand-in sends `sequence` 1, which operations requires.
    - `split-check` fails a domain that reaches a third-party provider.
    - The composition-root docs name `internal/config`.
    - `rules.Kinds` lists the kinds once, with a test.
    - A test pins `Emit`'s zero result.
    - The nits.

    The two context findings went into `design.md` and `exercise.md`.
  - **Delegation.** The executor (Opus) ran stages 9, 11, and 13–16; the editor (Sonnet) ran
    stage 17. The session read every diff.

## Next-focus

A `start` session for the path's last step: **the final validation** (`README.md`).

- Gather the evidence the README lists, each item on the running services where it can be:
  - two replicas in one delivery group share the work;
  - a shutdown drains in-flight handling;
  - a stop between commit and publish loses no event;
  - a redelivery is handled once;
  - outages converge;
  - `split-check` holds evidence 6.
- The close states the spike's answer for standards-lab/org's `goals.v1.messaging`.
