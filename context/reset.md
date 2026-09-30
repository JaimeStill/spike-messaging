# reset · skirmish

- **Status:** closeout
- **Session:** start
- **Branch:** skirmish

## Disposition

- **Integrated:**
  - The skirmish's world and rules are stated by exercise's `rules/doc.go`, `skirmish.go`, and
    README.
  - command's rules are stated by `decide/doc.go` and its README.
  - The alert, the revision, and the sequence are stated by each service's README.
  - The narration and the check are stated by courier's `scenario/doc.go`.
  - `exercise.md`'s "The skirmish, next" is deleted, and its deferred ideas keep a "Deferred"
    section.
- **Add or sharpen:**
  - `exercise.md`:
    - The world, the elements and their resolution, the services, and one round are rewritten
      for the skirmish: the seeded layout, squads and health, pursuit, two-round capture, hidden
      objectives and discovery, loss alerts, revisions and sequences, and command's eight rules.
    - Guideline 7 now reads: the ruleset grows only as far as the flow between the services
      needs.
    - A seed replays a run only together with its recorded orders.
    - The observer's view comes from exercise's API.
    - The rule names are a string contract.
  - `design.md`: the open question on change-only events now records the settled pattern, full
    state plus a producer's counter, and asks only whether go-messaging names it.
  - `README.md`: the capabilities cover the skirmish, `theater-check`, and `SEED`. The path is
    **the cleanup**, then **the final validation**.
  - New `cleanup.md`: the next step and its known inputs.
- **Culled:** `scripts/theater_check.py`, and a `.pyc` that was committed by accident. courier's
  `theater-check` scenario replaces them.
- **Validated:**
  - **Checkpoint A, first run.**
    - Seed 7 replayed identically, and seed 11 differed.
    - Fights lasted several rounds, and there were reinforcements and two-round captures.
    - The check was consistent.
    - The architect's findings (public objectives, the retreat's fire, recovering narration,
      objective naming) became the first re-plan: stages 8–13.
  - **Checkpoint A, second run.**
    - Scouts discovered the objectives, pursued retreats traded fire, and unpursued retreats
      escaped.
    - Seed 11 stalled on stale beliefs, which became the second re-plan: stages 14–18.
      - The loss alert and revised assessments.
      - Re-scouting.
      - The Python check ported to Go.
  - **Checkpoint A, third run.**
    - Seed 11 no longer stalled, and loss alerts and re-scouting showed.
    - Seeds 7, 11, and 23 were consistent.
    - Adjusts: the narration became round blocks by side, and "observer" replaced "umpire".
  - **Checkpoint B.**
    - The fixture went to 30 rounds, with an editor pass.
    - The full task set passed: build, vet, test, lint with sqlint, split-check, and
      integration.
    - Seeds 7, 11, and 23 finished in 30–34s with winners, and the check was consistent.
  - **Branch review** (the reviewer on Opus; the session verified the findings). All 11 were
    fixed.
    - The seed left `exercise.started`, and the objectives left the resolution. The observer
      reads both from the API.
    - A monotonic `revision` on assessments and a `sequence` on directives, with late-arrival
      tests.
    - A pursuer must have stayed in the cell.
    - The replay claim is stated precisely.
    - An element on an objective claims it first, and retreats skip own-held cells.
    - `Alert` raises an assessment only on a change, and theater-check uses struct keys.
    - The narration waits for a final-round revision, and the fight-cell difference is
      documented.
    - Both test gaps are closed.
    - One fix needed a follow-up: the narration now holds until its step begins.
    - Rerun:
      - The full task set passed.
      - Seed 7 twice live: identical, 34s each, and consistent.
  - **The editor pass** (Sonnet): prose only. I read the diff.

## Next-focus

A `start` session for the path's step 1: **the cleanup** (`context/cleanup.md`).

- Review the whole spike's architecture for every simplification and refinement:
  - the root libraries, the providers and engine, courier, and the four services;
  - begin with courier's `theater.go` and `theatercheck.go`, which the architect found tangled.
- Settle the refactor from the findings as a stage list.
- The cleanup's inputs are a start, not the bound.
- **Checkpoint.** The refactor changes no behaviour:
  - the full task set passes;
  - `SEED=7 mise run demo-theater` narrates as before;
  - `demo-theater-check` stays consistent.
