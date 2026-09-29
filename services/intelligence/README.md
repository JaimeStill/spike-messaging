# intelligence

The intelligence service keeps what each faction knows of the spike's exercise
(`context/exercise.md`). It fuses each faction's observations into an assessment. It was generated
from
[go-web-sdk-template](https://github.com/standards-lab/go-web-sdk-template) `template/v0.9.0`.

## Running

The service runs on the host against the repository's compose stack: Postgres, which holds the
`intelligence` database, and NATS with JetStream. From the repository root:

```sh
mise run up                 # start Postgres and NATS
mise run intelligence-serve   # run the service on 127.0.0.1:8082
```

## API

Mounted under `/api/intelligence`:

| Route | Action |
|-------|--------|
| `GET /{exercise}` | Each faction's assessment: its status, the last round it fused, its own elements, its contacts with the round each was seen and its age, and its objectives |

`/healthz` and `/readyz` are the probes. Readiness reports the database, the broker, the schema
service, and each reactor. The commands have no route: their inputs arrive as events.

## Events

The service consumes three event types, each through a subscription of its own, and decodes each
payload into its command's input, its own reading of the payload:

| Subscription | Event | Command |
|--------------|-------|---------|
| `intelligence-started` | `exercise.started` | `Open` both factions' assessments over the map |
| `intelligence-observed` | `exercise.round.observed` | `Observe`: fuse the faction's observation into its assessment |
| `intelligence-concluded` | `exercise.concluded` | `Close` the exercise's assessments |

Every command claims its event through the inbox, so a redelivery changes nothing. An input for
an assessment not open yet, as when a round's observation is handled before its start, is
redelivered after 250ms rather than refused.

The service emits `intelligence.assessment.issued`, whose subject is the exercise's ID:
`{exercise, faction, round, own, contacts: [{id, faction, kind, strength, at, seen, age}],
objectives: [{at, holder, known, seen, age}]}`. It emits one per faction per observed round.

Exercise's observation limits what a faction sees by kind and to the sector. Intelligence keeps a
contact at its last-seen cell, and drops it after `contact_rounds` rounds unseen or once a
friendly element sees that cell empty. An objective never seen has no known holder.

## Composition

`internal/app` builds one layer per file, and the lifecycle coordinator starts them in stage
order:

| Stage | Service |
|-------|---------|
| 0 | `database`, and `broker`, which connects and provisions the shared stream at start |
| 1 | `schema`: go-database's admin service migrates the `messaging` set, then `intelligence` |
| 2 | `messaging` and `intelligence` verify their statements |
| 3 | `relay`, which publishes the outbox |
| 4 | `started`, `observed`, and `concluded`, the consumers |
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
