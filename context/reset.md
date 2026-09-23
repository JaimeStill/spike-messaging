# reset · init

- **Status:** closeout
- **Session:** init
- **Branch:** main

## Disposition

- **Add or sharpen:** `README.md` holds the question, the capabilities, the path, and the final
  validation.
  `design.md` and `api.md` carry the served project's messaging notes over as the spike's own.

## Next-focus

Step 1 of the path in `README.md`: build `core/event` and `core/reactor`, the lowest pieces,
which everything else depends on:

- Set up the module (`github.com/JaimeStill/spike-messaging`) and a mise toolchain (go 1.27,
  golangci-lint 2.13.2, and the build, vet, test, and lint tasks).
- `core/event`: settle adopting `cloudevents/sdk-go/v2/event` or writing the type
  (`design.md`, "Open questions"), and record the loser as a rejected alternative.
- `core/reactor`: the source contract, the reactor as a lifecycle component (`api.md`), and
  `Every`. Settle whether it stands alone or belongs in `lifecycle`, and start the evidence on
  lifecycle registration (`design.md`).
- Checkpoint: a small program runs an `Every` reactor on the coordinator and drains it on
  shutdown.
