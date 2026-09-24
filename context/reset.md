# reset · messaging-memory-conformance

- **Status:** closeout
- **Session:** start
- **Branch:** messaging-memory-conformance

## Disposition

- **Integrated:**
  - `api.md`'s `messaging` section and its `messaging/memory` entry are gone. The package
    documentation (`go doc ./messaging`) states that API, and `messaging/messagingtest` holds the
    conformance suite. The composition section now cites `scenario/coordinator.go`.
  - courier replaces `cmd/every` and `cmd/group` (`go run ./cmd/courier`).
- **Add or sharpen:**
  - `design.md` gains two decisions: the source decides what a handler error means and the
    handler keeps the source's deadline; and Name is both the durable and the delivery group,
    with a separate `Group` rejected.
  - `design.md` adds courier's lifecycle evidence, and the reactor-stage question now carries
    `group`'s staging.
  - `design.md` gets a new section on the CLI layout's fit and its three differences from clutch.
  - `design.md` adds open questions for the nats provider (a late ack, one fetch per pull,
    subject tokens for `Types`), deduplication in the suite, and the import check allowing
    `_test.go` files.
  - `README.md` removes the finished step from the path and names `messagingtest` and courier
    under the capabilities.
- **Culled:** `design.md` drops the open question on handler errors and deadlines; the step
  settled it.
- **Retained:** `api.md`'s outbox, nats, and composition sections, none of them built yet.
- **Validated:**
  - Checkpoint A: the conformance suite passes on memory (`go test -race -count=5 -v
    ./messaging/...`). Mutating the Permanent check and settle's expiry each failed its case.
    - Adjust `a230516`: `slices.Contains` in the filter checks.
    - Adjust `f6e52c0`: memory split into broker, consumer, and source layers.
  - Checkpoint B: `cmd/group` showed the split, the retry, the terminate, and both drains. The
    program has since been replaced.
  - Checkpoint C (the final validation): `mise run build ::: vet ::: test ::: lint` and
    `go test -race -count=5 ./...` pass. Then `go build -o bin/courier ./cmd/courier`:
    - Each of `every`, `group`, `retry`, `permanent`, and `drain` runs clean and exits 0.
    - `every --fail-after 2` exits 1 with `run: reactor: tick 2 failed`.
    - `drain --work 10s --grace 1s --drain 3s` exits 1 with `worker: reactor: handlers cancelled
      after grace 1s`.
    - `every --grace 5s --drain 5s`, an unknown scenario, an unknown broker, and an unknown flag
      each exit 2.
    - An interrupt mid-scenario narrates the drain, reports a grace cancellation, and exits 1.
    - Adjust `3220506`: an interrupted scenario's drain was silent and dropped its error.
  - Adjust `5064b99`, the branch review's findings:
    - The contract states the start position, the binding rule, and one delivery per member.
    - The suite gains four cases (`StartsAtStreamBeginning`, `BindingMustMatch`,
      `MaxDeliverBoundsExpiry`, and a two-type filter), and a mutation for each fails its case.
    - memory normalizes its stored configuration.
    - An interrupted scenario's error names the scenario, and the acknowledgement check watches
      a quiet window.

## Next-focus

Step 1 of the path in `README.md`, `messaging/outbox` on Postgres: the emitter, the relay, the
migration set, and a compose stack. It proves evidence 2: a stop between commit and publish
loses no event. courier gains an outbox scenario, whose need is the Postgres stack. Before
building, settle the open outbox questions in `design.md`:

- where the outbox writer lives
- whether the relay polls or listens
- whether an inbox table backs idempotency
- whether the outbox can enforce `event.Tx`

The mise task invocation takes `:::` between tasks (`mise run build ::: vet ::: test ::: lint`);
step 1's record wrote it without them.
