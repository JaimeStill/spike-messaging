# The cleanup

The next step: a holistic architecture review of the whole spike, then the refactor it settles,
before the final validation cites the code as evidence. The architect asked for it after the
skirmish, finding courier's `theater` and `theater-check` tangled and short of the rigor the
rest of the spike keeps. The review identifies every simplification and refinement across the
modules; the step plans the refactor from its findings.

## Known inputs

The skirmish's branch review and its session surfaced these. They start the review; they don't
bound it.

- **courier's `scenario/theater.go`** (about 1200 lines) is one `narrator` that decodes events,
  keeps per-faction state, assembles round blocks, renders them, and measures chain latency.
  Its string keys (`movement.key()`, `stamp`) are built with `fmt.Sprint`.
- **`scenario/theatercheck.go`** is a fourth reading of the same payloads beside the theater's,
  the assessments scenario's, and the directives stand-in's, and it mirrors exercise's sight rule
  (`sight`) and capture rounds (`captureRounds`) by hand, which can drift silently.
- **Leftovers from hiding the objectives:** `route.Map.Objectives()` has no caller outside its
  test, and command's and operations' `Sector.Objectives` are always empty. intelligence's
  `Objective.Known` is always true, kept for the payload's shape, so command's `!o.Known` branch
  is dead.
- **operations** keeps `Targets` and `Rules` as parallel maps with parallel deletes, and
  `directive_round`, which no longer guards anything since the directive's `sequence` does.
- **intelligence** stores the picture's grid through a `storedPicture` wrapper and a `json:"-"`
  field.
- **The rule names** are a string contract across command, operations, and courier.
  `exercise.md` lists them; the code documents them nowhere together.
