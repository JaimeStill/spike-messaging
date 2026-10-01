# reset · final-validation

- **Status:** closeout
- **Session:** start
- **Branch:** final-validation

## Disposition

- **The question:** can one broker-agnostic event and reactor contract, built on CloudEvents with
  outbox emission, run a service's reactors on NATS JetStream and on an in-memory provider, with
  no broker import outside the composition root and the provider?
- **The answer: yes.**
  - Four services play a 30-round, two-faction exercise on JetStream through the agnostic
    contract.
  - The same contract passes the conformance suite on the in-memory provider.
  - Each of the eight items of evidence holds. `README.md`, "The answer", cites the test or the
    validate task behind each. Two of them are proven by tests and not on the running services:
    - Evidence 2: a stop between commit and publish loses no event.
    - Evidence 3: a redelivery is handled once.

    A kill on the running services landing inside that window depends on timing, and none did.
  - Evidence 7's composite Postgres and blob case waits on go-storage.
- **What go-messaging's API gains:**
  - `Subscription.Start`, a start position that is binding configuration.
  - A finite `MaxDeliver` for inputs that can arrive before their start. It bounds every failure
    and drops silently, so it needs an error hook or a dead-letter path.

  `design.md`'s decisions and open questions are the input for standards-lab/org's
  `goals.v1.messaging`.
- **Integrated:**
  - The final validation is built.
  - `README.md`: "The final validation" is replaced by "The answer", and the path is complete.
  - `exercise.md`: "Smoothed in the final validation" is deleted. Each of its items is either
    settled or moved to `design.md`'s open questions.
- **Add or sharpen:**
  - `design.md`, new decisions:
    - the start position;
    - the bound on early inputs and its cost;
    - exercise skips late orders.
  - `design.md`, open questions:
    - The error-hook question now covers a bounded consumer.
    - New: a durable's binding configuration needs recreation to change.
    - New: a first boot does not play an exercise already under way, and per-consumer creation
      has a window.
    - New: the `*-started` subscriptions retry in a tight, unbounded loop.
  - `exercise.md`:
    - Evidence 4, 5, and the principle now cite `validate-replicas` and `validate-outages`, the
      NATS outage included.
    - It records what the final validation settled.
    - The tasks list includes the validate tasks.
  - `README.md`: the `messaging` capability names the start position.
- **Validated:**
  - **Checkpoint A** (stages 1–5):
    - From a fresh stack, seed 7 ran three times. Every run concluded with blue holding every
      objective after round 22, and `demo-theater-check` found each consistent.
    - operations booted against a stream of 469 retained messages with its consumers and
      database deleted. It consumed and published nothing. The other services' restarts consumed
      nothing either.
    - No service logged `event refused`.
    - The full task set passed: build, vet, test, lint, split-check, and integration.
  - **Checkpoint B** (stage 6):

    | Run | Result | Detail |
    |---|---|---|
    | `SEED=7 validate-replicas` | 14/0 | split 22/23 by round 10; drain 108ms |
    | `SEED=11 validate-replicas` | 14/0 | split 22/24 by round 10; a draw at the limit |
    | `SEED=7 validate-outages` | 57/0 | operations, command (SIGKILL), intelligence, exercise, and nats each converged |
  - **Final:**
    - The editor pass ran (stage 8).
    - The full task set passed.
    - The question's answer was stated against evidence 1–8.
  - **Branch review** (the reviewer on Opus; the session verified every finding): 11 findings, no
    blocking bug. Fixed as checkpoint adjusts, after which the full task set and both validate
    tasks passed again (14/0 and 57/0):
    - `DurableResumes` runs under both start positions.
    - `RecordOrders` refuses a round below 1 before it skips a late order, and its test compares
      the stored orders.
    - The READMEs and the subscriptions' comments state the bound's cost, the first boot, and the
      binding break.
    - The scripts refuse occupied ports before resetting the stack, which a guard run confirmed.
      Their `set -e` exits now report the summary, and the drain bound allows the exit poll.
    - The doc nits.

    The context finding went into the notes. The tight retry loop, which predates the branch, is a
    new open question.
  - **Delegation:**
    - The executor (Opus) ran stages 1 and 6.
    - The session ran stages 2–5 and every adjust.
    - The editor (Sonnet) ran stage 8, and the session fixed its rewrap.
    - The session read every diff.

## Next-focus

The spike is complete; nothing further runs here. A `plan` session in standards-lab/org reads this
closeout for `goals.v1.messaging`. It decides with the architect what go-messaging and go-core
take from it, using `README.md`'s answer and `design.md`'s decisions and open questions. It then
confirms this repository is pushed, archives it, and keeps the catalog entry as the pointer.
