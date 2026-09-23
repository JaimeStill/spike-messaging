# reset · core-event-and-reactor

- **Status:** closeout
- **Session:** start
- **Branch:** core-event-and-reactor

## Disposition

- **Integrated:** the `event` and `reactor` sections of `api.md`. The package documentation
  (`go doc ./core/event`, `go doc ./core/reactor`) now states that API. The composition section
  stays, updated to `event.Tx`, `reactor.Grace`, and `Monitor`.
- **Add or sharpen:**
  - `design.md` records two decisions: the envelope type is the spike's own, with sdk-go
    rejected, and `reactor` is a standalone package.
  - `design.md` records the lifecycle-registration evidence from `cmd/every`.
  - `design.md` replaces the two answered open questions with three new ones: what a handler
    error means for each source, together with the per-message deadline; `Tx` not enforcing a
    transaction; and header encoding and structured mode.
  - `README.md` removes step 1 from the path.
- **Retained:** the rest of `api.md`, which covers `messaging`, the outbox, and the providers,
  none of them built yet.
- **Validated:**
  - Checkpoint (the final validation). `mise run build vet test lint` and
    `go test -race -count=5 ./...` pass. Then `go build -o bin/every ./cmd/every`:
    - `bin/every -work 2s`, interrupted mid-tick: the tick completes, and the program exits 0.
    - `bin/every -work 5s -drain 2s -grace 1s`, interrupted: "tick cancelled", then
      `ticker: reactor: handlers cancelled after grace 1s`, exit 1.
    - `bin/every -fail-after 3`: `run: reactor: tick 3 failed`, exit 1.
    - `bin/every -grace 5s -drain 5s`: exit 2.
  - Adjust `4afc46d`: the two-phase `Grace` drain replaces `DrainTimeout` (`ab29a17`).
  - Adjust `a1b15df`: the branch review's findings, including an error lost around the signal
    and stale `Every` ticks. The new tests fail against the unfixed code.

## Next-focus

Step 1 of the path in `README.md`: `messaging`, `messaging/memory`, and the conformance suite,
covering publish, subscribe, delivery groups, and acknowledge, redeliver, and terminate. Before
building it, settle:

- What a handler error means for each source (`design.md`, "Open questions"), and whether
  `Every` follows the rule.
- Whether the reactor's handler wrapper keeps a per-message deadline from the source, such as
  `AckWait`.

`core/event`'s `Encode` and `Decode` are the memory provider's codec. Registering a subscription
reactor adds to the lifecycle-registration evidence in `design.md`.
