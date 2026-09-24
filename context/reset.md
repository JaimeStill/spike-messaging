# reset · messaging-outbox

- **Status:** closeout
- **Session:** start
- **Branch:** messaging-outbox

## Disposition

- **Integrated:**
  - `api.md`'s `messaging/outbox` section is gone. The package documentation
    (`go doc ./messaging/outbox`, `go doc ./messaging/outbox/postgres`) states that API.
  - The composition section now shows the outbox's wiring: the engine, the emitter, the relay at
    the root stage, and `Claim`.
  - The courier paths in `api.md` and `design.md` now point under `courier/`.
- **Add or sharpen:**
  - `design.md` gains six decisions, each with its rejected alternatives:
    - the transaction is enforced at compile time through `event.Tx`
    - the outbox is engine-agnostic, and an engine is required and defines every statement
    - the SQL runs through sqlate's `query` package
    - the relay polls and is a reactor source
    - an inbox table backs idempotency
    - the workspace layout, with `split-check` guarding the root
  - `design.md`'s "Outbox sequencing" now describes the relay's single transaction: claim,
    publish, and mark. It records the departure from spike-blobfs's two-step, the `seq`-order
    caveat, and where evidence 2 is proved.
  - `design.md` adds open questions: how the relay reports errors, a quarantine for rows that can
    never publish, retention, `Claim` against `AckWait` in the nats step, a sibling package for
    the inbox, the `Timeout` default, and `require` lines at promotion. The import-check question
    now points at `split-check`.
  - `README.md` describes the three-module workspace and the engine module, and removes this step
    from the path.
  - `CLAUDE.md`'s **Modules** bullet describes the workspace.
- **Culled:** `design.md` drops the open questions on enforcing `event.Tx`, on where the writer
  lives, on polling versus listening, and on the inbox table. This step settled all four.
- **Retained:** `api.md`'s nats section, which isn't built yet.
- **Validated:**
  - **Checkpoint A** (evidence 2): `mise exec -- go test -race -tags integration -v`, run on the
    outbox.
    - `TestStopBetweenCommitAndPublishLosesNoEvent` passes, with 19 tests in all.
    - Beginning the transaction on the source's context fails the shutdown test.
    - Marking and committing before the publish fails the evidence 2, retry, and republish tests.
  - **Adjust `0fb6c0e`**: the SQL moved into sqlate statement files.
    - `Verify` is tested, and sqlint joined the lint task through `go tool`.
    - sqlint's first run flagged a `jsonb` cast, and the cast was removed.
  - **Re-plan** after checkpoint A: the courier module (stage 7) and the engine seam (stage 8),
    which puts the Postgres engine in a module of its own.
  - **Checkpoint A2**: the full suite and both mutations rerun, with the golden hashes unchanged.
    `split-check` caught a planted pgx import that the workspace build accepted.
  - **Checkpoint B**, the final validation:
    - `mise run build ::: vet ::: test ::: lint ::: split-check`, `mise run integration`, and
      `go test -race -tags integration -count=5` pass across all three modules.
    - `bin/courier scenario outbox` exits 0 and narrates "5 rows pending", five deliveries,
      "drained cleanly", "each of 5 events delivered once", "0 rows pending", and the dropped
      database.
    - With the stack down, the scenario fails its Postgres need and exits 1.
    - `--events 0` exits 2.
    - No scratch database is left behind.
  - **Adjust `2820449`**, the branch review's findings:
    - The relay's transaction is bounded at twice `Timeout`, and a mark that changes no row
      fails the pass.
    - `New` rejects a transaction declared required on `Emit` or `ClaimInbox`.
    - A failed `CREATE DATABASE` is followed by a drop.
    - `split-check` became an allow-list over `-test -tags integration`, and probes in root code
      and in a tagged test each fail it.
    - Four doc misstatements were fixed.
    - Six tests were added, and the shutdown test now waits on a signal.
    - Mutations of the bound and of the row check each fail their test.

## Next-focus

Step 1 of the path in `README.md`, `messaging/nats`. It covers conformance on JetStream,
deduplication on the event `id` (`Nats-Msg-Id`, which joins the conformance suite), stream
provisioning, and the native request and reply. It proves evidence 1 on NATS, and evidence 8.
Before building, settle `design.md`'s nats open questions:

- the late ack after `AckWait`
- one fetch per pull
- a subject-token rule for `Types`
- who provisions a stream, and how readiness and drain run through the coordinator
- how `Outbox.Claim` interacts with `AckWait`

At SETTLE, decide whether the provider becomes a module of its own, as the outbox's Postgres
engine is, because it pulls in nats.go. If it does, extend `go.work`, `MODULES`, and
`split-check`. courier gains the nats broker, and a compose service for NATS.
