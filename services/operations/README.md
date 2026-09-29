# operations

The operations service maneuvers the elements of the spike's exercise (`context/exercise.md`). It
steps each element toward the target its faction's directive sets, and issues each faction's
orders for the next round. It was generated from
[go-web-sdk-template](https://github.com/standards-lab/go-web-sdk-template) `template/v0.9.0`.

## Running

The service runs on the host against the repository's compose stack: Postgres, which holds the
`operations` database, and NATS with JetStream. From the repository root:

```sh
mise run up                 # start Postgres and NATS
mise run operations:serve   # run the service on 127.0.0.1:8084
```

## API

Mounted under `/api/operations`:

| Route | Action |
|-------|--------|
| `GET /{exercise}` | Each faction's operation: its status, the last round it acted on, its elements, and each element's target |

`/healthz` and `/readyz` are the probes. Readiness reports the database, the broker, the schema
service, and each reactor. The commands have no route: their inputs arrive as events.

## Events

The service consumes four event types, each through a subscription of its own, and decodes each
payload into its command's input, its own reading of the payload:

| Subscription | Event | Command |
|--------------|-------|---------|
| `operations-started` | `exercise.started` | `Open` both factions' operations over the map |
| `operations-directives` | `command.directive.issued` | `Assign` the targets a faction's directives name |
| `operations-observed` | `exercise.round.observed` | `Maneuver`: record the elements and issue the next round's orders |
| `operations-concluded` | `exercise.concluded` | `Close` the exercise's operations |

Every command claims its event through the inbox, so a redelivery changes nothing. An input for
an operation not open yet, as when a round's observation is handled before its start, is
redelivered after 250ms rather than refused.

The service emits `operations.orders.issued`, whose subject is the exercise's ID:
`{exercise, faction, round, orders: [{element, steps: [{sector, x, y}]}]}`. It emits one per
faction per observed round, for the round after it, even with no orders, and none past the round
limit. When a directive changes the plan for orders already issued, it emits them again for the
same round, and exercise keeps the last it records. A directive's payload is
`{exercise, faction, round, directives: [{element, target}]}`, where a null target holds the
element.

Each element steps toward its target along a shortest path, found by breadth-first search over
open cells and gates (`domain/operations/route`), up to its moves per round, and no two of a
faction's elements end a round on one cell.

## Composition

`internal/app` builds one layer per file, and the lifecycle coordinator starts them in stage
order:

| Stage | Service |
|-------|---------|
| 0 | `database`, and `broker`, which connects and provisions the shared stream at start |
| 1 | `schema`: go-database's admin service migrates the `messaging` set, then `operations` |
| 2 | `messaging` and `operations` verify their statements |
| 3 | `relay`, which publishes the outbox |
| 4 | `started`, `directives`, `observed`, and `concluded`, the consumers |
| root | `server` |

The drain runs in reverse. The consumers raise the service's orders, so they sit above the relay:
the drain stops them first, and the relay's last pass publishes what they committed. The broker
and database close last.

## Configuration

Configuration is layered: `config.json`, `config.<OPERATIONS_ENV>.json`, and the secrets files,
with `OPERATIONS_*` environment variables applied last. `operations:serve` sets
`OPERATIONS_ENV=local`, so `config.local.json` points the service at the compose stack. The
service adds three blocks to the template's:

- `database`: go-database's connection block.
- `messaging`: `messaging.Config`, the service's CloudEvents `source` and the relay's
  `relay_poll`.
- `nats`: `nats.Config`, the NATS URL, the connection's name, and the stream and prefix it shares
  with the other exercise services, with the stream's `max_age`, which each service must set
  alike.

## Testing

From the repository root, `mise run test` runs the unit tier and `mise run integration` runs the
integration tier across every module. For this service, the integration tier covers two things:

- the domain's commands, on a scratch database;
- the built service, against the compose stack, on a scratch database and stream of its own,
  driven through the events it consumes.
