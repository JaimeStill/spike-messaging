# reset · messaging-nats

- **Status:** closeout
- **Session:** start
- **Branch:** messaging-nats

## Disposition

- **Integrated:**
  - `api.md`'s Providers section is gone. The package documentation (`go doc ./messaging/nats`)
    now states the nats provider's API.
  - The composition section now shows the nats broker's construction, its lowest-stage
    registration, and its native handle.
- **Add or sharpen:**
  - `design.md` gains eight decisions, each with its rejected alternatives:
    - `messaging/nats` is a module of its own, tested against compose NATS
    - the subject-token rules `CheckType` and `IsToken`
    - deduplication on source and id
    - provisioning in the broker's constructor, with the broker owning its connection
    - bind at `Receive`, one pull at a time, with `AckMargin` and the late-outcome drop
    - verbatim header values
    - request and reply through `Broker.Conn()`
    - `Claim` on the handler's context
  - "Outbox sequencing" now names source and id. The lifecycle evidence gains a second case of
    readiness lagging: `request` waits on its responder's readiness.
  - `design.md` adds open questions: registering the nats broker on a coordinator, and the
    stream's unbounded retention. The relay's error-reporting question now covers a nats source's
    failing pulls too.
  - `README.md` describes the four-module workspace and `split-check`'s second check, and removes
    this step from the path.
  - `CLAUDE.md`'s **Modules** bullet names `messaging/nats`.
- **Culled:**
  - `design.md` drops these open questions, all settled by this step:
    - the late ack
    - one fetch per pull
    - the subject-token rule
    - who provisions a stream
    - deduplication joining the conformance suite
    - the codec's percent-encoding for NATS
    - `Claim` against `AckWait`, whose `REPEATABLE READ` caveat moved into its decision
  - **Falsified:** the plan keyed deduplication on the id alone. The branch review showed that
    CloudEvents identifies an event by source and id, and the inbox already claims on both.
- **Retained:**
  - `README.md`'s demonstration-service path entry, until the plan session below revises it.
- **Validated:**
  - **Checkpoint A** (evidence 1 on NATS): `mise exec -- go test -race -tags integration -v
    ./messaging/nats/...`, with the stack up.
    - All 18 conformance cases passed on JetStream and on memory.
    - Ten repeated runs were clean.
    - Removing the late-outcome drop fails `MaxDeliverBoundsExpiry` and `AckWaitRedelivers`.
    - `TestClaimAcrossAckWait` passes.
  - **Checkpoint B**, the final validation:
    - `mise run build ::: vet ::: test ::: lint ::: split-check`, `mise run integration`, and
      `-race -count=5` pass across all four modules.
    - `bin/courier --broker nats scenario` runs `every`, `group`, `retry`, `permanent`, `drain`,
      `outbox`, and `request`, and each exits 0. `request` answers each of its requests through
      `Broker.Conn()` (evidence 8).
    - No stream or consumer is left behind.
    - `scenario request` on memory exits 1 on its need, and `--events 0` exits 2.
    - With NATS stopped, a scenario exits 1 on its need.
    - A planted nats.go import in `courier/scenario` fails `split-check`.
  - **Adjust `35ff810`**, which settled all ten of the branch review's findings:
    - Deduplication keys on source and id, and `Deduplicates` proves that another source reusing
      an id still delivers.
    - `DrainKeepsAck` and courier's drain now watch past a short `AckWait`. A dropped drained ack
      fails both.
    - A consumer deleted under a running handler ends `Receive`
      (`TestDeletedConsumerEndsReceive`), and removing the check fails that test.
    - `IsToken` now rejects `/` and `\`.
    - `Shutdown` closes the connection when its context ends. `ErrInvalidOption` is retried.
    - `every` needs nothing, and `Scenarios` takes a `Dependencies` struct.
    - `SingleTypeFilter` is added.
    - The full suite, `-count=5`, and every nats scenario pass again.

## Next-focus

A `plan` session to design the spike's final demonstration: two services whose state stays in
step through events. It replaces the README's single demonstration service. Plan it before any
code, write its note, and revise the path.

The architect's governing principle: **no service's availability or internal function may depend
on another service's.** The event layer decides where and how services integrate, and a
degraded or unavailable peer must never bring a service's operations to a halt. Design and
validate against that.

The agenda:

- **What the events carry.** Events that carry state, so a consumer never calls back, or events
  that only notify. This choice sets the event schema and `dataschema`.
- **Ordering and versioning.** A projection must absorb duplicate and out-of-order delivery,
  for example with a per-entity version applied only when it is newer.
- **Deletes and bootstrap.** Deletes need tombstones. A new consumer catches up from the stream,
  but only within the stream's retention, which ties into the open question on stream limits.
- **The domain.** Something small where the second service plainly needs the first's state, with
  the composite Postgres and blob operation on the producer.
- **Evidence 3 through 7 across the two services**, and how the final validation shows the
  principle, such as the consumer staying available while the producer is down and converging
  once it returns.
- **The steps.** Likely a producer step and then a consumer step with the final validation.
  Where the services live in the workspace, and what they take from go-web-service.
