# intelligence

The intelligence service keeps what each faction knows of the spike's exercise
(`context/exercise.md`). It fuses each faction's observations into an assessment. It was generated
from
[go-web-sdk-template](https://github.com/standards-lab/go-web-sdk-template) `template/v0.9.0`.

## Running

The service runs on the host against the repository's compose stack: Postgres, which holds the
`intelligence` database, and NATS with JetStream. From the repository root:

```sh
mise run up                   # start Postgres and NATS
mise run intelligence-serve   # run the service on 127.0.0.1:8082
```

## API

Mounted under `/api/intelligence`:

| Route | Action |
|-------|--------|
| `GET /{exercise}` | Each faction's assessment: its status, the last round it fused, its own elements, its contacts with the round each was seen and its age, the objectives it has seen, and the cells its elements have had in sight |

`/healthz` and `/readyz` are the probes. Readiness reports the database, the broker, the schema
service, and each reactor. The commands have no route: their inputs arrive as events.

## Events

The service consumes four event types, each through a subscription of its own, and decodes each
payload into its command's input, its own reading of the payload:

| Subscription | Event | Command |
|--------------|-------|---------|
| `intelligence-started` | `exercise.started` | `Open` both factions' assessments over the map |
| `intelligence-observed` | `exercise.round.observed` | `Observe`: fuse the faction's observation into its assessment |
| `intelligence-alerts` | `exercise.objective.lost` | `Alert`: record the objective's new holder in the faction's assessment |
| `intelligence-concluded` | `exercise.concluded` | `Close` the exercise's assessments |

Every command claims its event through the inbox, so a redelivery changes nothing. An input for
an assessment not open yet, as when a round's observation is handled before its start, is
redelivered after 250ms rather than refused.

The service emits `intelligence.assessment.issued`, whose subject is the exercise's ID:
`{exercise, faction, round, revision, own, contacts: [{id, faction, kind, strength, health, status, at,
seen, age}], objectives: [{at, holder, seen, age}], explored: [{sector, x, y}]}`. It emits
one per faction per observed round, and one more when an alert changes an assessment that already
covers the alert's round. Exercise raises `exercise.objective.lost` after a round resolves and
before it is observed, and the two arrive on separate subscriptions. When the assessment has
reached the alert's round, the alert issues a fresh assessment of that round with the objective's
new holder; otherwise it only saves, and the round's observation issues the assessment carrying
it. A later sighting than the alert's wins. An alert that restates what the assessment already
lists changes nothing, and issues nothing.

`revision` counts the assessments issued for one faction's assessment in an exercise, across
observations and alerts, from 1. An observation's assessment and an alert's of the same round
carry different revisions, and they can arrive out of order, so a consumer decides an assessment
only when its revision is higher than the last it decided for that exercise and faction.

Exercise's observation limits what a faction sees by kind, a squad one cell and a scout two, and
to the sector. Intelligence keeps a contact at its last-seen cell, and drops it after
`contact_rounds` rounds unseen or once a friendly element sees that cell empty. The map carries no
objectives: a faction discovers one when an observation reports it in sight, and the picture lists
it from then on, with its last-seen holder and its age. `explored` holds
every cell inside a sector's grid that one of the faction's own elements has had in sight, over all
the rounds so far, sorted by sector, y, then x.

## Composition

`internal/app` builds one layer per file, and the lifecycle coordinator starts them in stage
order:

| Stage | Service |
|-------|---------|
| 0 | `database`, and `broker`, which connects and provisions the shared stream at start |
| 1 | `schema`: go-database's admin service migrates the `messaging` set, then `intelligence` |
| 2 | `messaging` and `intelligence` verify their statements |
| 3 | `relay`, which publishes the outbox |
| 4 | `started`, `observed`, `alerts`, and `concluded`, the consumers |
| root | `server` |

The drain runs in reverse. The consumers raise the service's assessments, so they sit above the
relay: the drain stops them first, and the relay's last pass publishes what they committed. The
broker and database close last.

## Configuration

Configuration is layered: `config.json`, `config.<INTELLIGENCE_ENV>.json`, and the secrets files,
with `INTELLIGENCE_*` environment variables applied last. `intelligence-serve` sets
`INTELLIGENCE_ENV=local`, so `config.local.json` points the service at the compose stack. The
service adds four blocks to the template's:

- `database`: go-database's connection block.
- `messaging`: `messaging.Config`, the service's CloudEvents `source` and the relay's
  `relay_poll`.
- `nats`: `nats.Config`, the NATS URL, the connection's name, and the stream and prefix it shares
  with the other exercise services, with the stream's `max_age`, which each service must set
  alike.
- `intelligence`: the service's own block. `contact_rounds`, overridden by
  `INTELLIGENCE_CONTACT_ROUNDS`, is the number of rounds a contact stays in an assessment unseen
  before it drops; it defaults to 3 and must be at least 1.

## Testing

From the repository root, `mise run test` runs the unit tier and `mise run integration` runs the
integration tier across every module. For this service, the integration tier covers two things:

- the domain's commands, on a scratch database;
- the built service, against the compose stack, on a scratch database and stream of its own,
  driven through the events it consumes.
