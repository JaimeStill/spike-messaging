# reset · init

- **Status:** closeout
- **Session:** init
- **Branch:** main

## Disposition

- **Add or sharpen:** `README.md` holds the question, the capabilities, and the final validation.
  `design.md` and `api.md` carry the served project's messaging notes over as the spike's own.

## Next-focus

Build `core/event` and `core/reactor`, the lowest pieces, which everything else depends on:

- Set up the module (`github.com/JaimeStill/spike-messaging`) and a mise toolchain (go 1.27,
  golangci-lint 2.13.2, and the build, vet, test, and lint tasks).
- `core/event`: settle adopting `cloudevents/sdk-go/v2/event` or writing the type
  (`design.md`, "Open questions"), and record the loser as a rejected alternative.
- `core/reactor`: the source contract, `Register` on go-core's lifecycle coordinator, and
  `Every`. Settle whether it stands alone or belongs in `lifecycle`.
- Checkpoint: a small program runs an `Every` reactor on the coordinator and drains it on
  shutdown.
