# reset · command

- **Status:** closeout
- **Session:** start
- **Branch:** command

## Disposition

- **Add or sharpen:**
  - `exercise.md`:
    - command is built, and the note records where each rule is enforced: path distance,
      standing capture targets, and the directive's `rule` and `contact`.
    - The rules gain attrition and pinning, in place of the instant engagement and its tie.
    - exercise emits `exercise.round.resolved`, and "One round" lists it.
    - operations applies the newest directive whenever it arrives.
    - The note describes the theater narration, the skirmish fixture, and the 50ms local poll.
    - Unbounded redelivery names command.
    - A new section, "The skirmish, next", holds the settled redesign and the deferred ideas.
    - The notes for command that intelligence's assessments raised are integrated into command's
      entry.
  - `README.md`:
    - The capabilities list gains command, the runtime's traffic logging, and courier's
      `theater`.
    - The path is now **the skirmish**, then **the final validation**.
  - `design.md`:
    - The `Runtime` entry records the traffic logging.
    - A new open question: a change-only event must carry its full state, so a consumer that
      skips stale input loses nothing.
  - `CLAUDE.md`: the modules paragraph names `services/command`.
- **Integrated:** command's API, stages, events, and rules are stated by
  `services/command/README.md` and its package documentation (`go doc` on
  `./services/command/domain/command` and `.../decide`). The theater narration is stated by
  `courier/scenario/theater.go`, and the skirmish by exercise's README.
- **Validated:**
  - **Checkpoint A, command runs by hand.** The architect ran the four services and the theater
    by hand. command re-decided after contacts, captures, and losses, and its log showed its
    traffic.
  - **Checkpoint B, the first final validation.** The full task set passed across every module.
    A 50-round `demo-theater` on command was consistent over 102 assessments.
  - **Re-plan: stage 3, broker traffic in the service logs**, at the architect's request. The new
    integration test sees a published event, a handled delivery, and a repeat.
  - **Re-plan: checkpoint C, the demonstration.** The additions:
    - attrition;
    - `exercise.round.resolved`;
    - the skirmish fixture, whose symmetry test fails when the symmetry is broken;
    - the 50ms local poll;
    - courier's `theater` narration, with `demo-theater` on the skirmish.
    - Adjusts:
      - The narration shows a round's orders when the round resolves, the ones exercise applied,
        and a pursuit reads as one engagement.
      - Squads are pinned in a fight, so fights last several rounds.
      - courier's listing test names the new scenario.
    - A skirmish run took about 17s: red won holding every objective, and `demo-theater-check`
      was consistent over 28 assessments.
  - **Branch review** (reviewer on Opus; the session verified all seven findings).
    - Fixed:
      - operations applies a late directive (migration 0002, `directive_round`), where it used
        to skip one behind its last observation and lose the change for good. The new test
        fails on the old rule.
      - `demo-theater-check` defaults to the latest exercise of any name.
      - `demo-theater` fails when its narration fails.
      - The ledger names what its assessed→directed figure counts.
      - A stale comment in decide's test.
    - Recorded: the `Resolution` missing from the round history, the narration reading orders by
      stream order, and command's unbounded redelivery.
    - Rerun: the full task set passed, and a skirmish on the architect's services was
      consistent. Those services predate the operations fix; its integration test proves it.
  - **The editor passes** (Sonnet, twice): prose only. I read both diffs.

## Next-focus

A `start` session for the path's step 1: **the skirmish** (`context/exercise.md`, "The skirmish,
next").

- Redesign the demonstration's world and rules as the note settles:
  - one 13×13 grid;
  - three squads of four operators with percentage health, and two scouts, a side;
  - mirrored random objectives and starting positions from a stored seed;
  - squads see 1 cell and move 1; scouts see 2 and move 2;
  - seeded fights;
  - command's retreat, which draws a free volley and costs the squad its next round, and its
    reinforcement;
  - two-round capture;
  - the narration's new step titles.
- Settle at SETTLE:
  - how operators and health appear in the payloads, with `strength` kept as the health total;
  - how the seed is chosen and stored;
  - how the retreat order reaches exercise.
- Revise guideline 7, and keep the `Resolution` in the round history.
- Restart operations before any live run, so its migration 0002 applies.
- **Checkpoint.** A seeded skirmish plays differently per seed and the same for one seed. It
  lasts well under a minute, shows fights over several rounds, a retreat, a reinforcement, and a
  two-round capture, and `demo-theater-check` stays consistent.
