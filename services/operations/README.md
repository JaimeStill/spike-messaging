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
mise run operations-serve   # run the service on 127.0.0.1:8084
```

## API

Mounted under `/api/operations`:

| Route | Action |
|-------|--------|
| `GET /{exercise}` | Each faction's operation: its status, the last round it acted on, the sequence of the last directive it applied, its elements, and each element's target and rule |

`/healthz` and `/readyz` are the probes. Readiness reports the database, the broker, the schema
service, and each reactor. The commands have no route: their inputs arrive as events.

## Events

The service consumes four event types, each through a subscription of its own, and decodes each
payload into its command's input, its own reading of the payload:

| Subscription | Event | Command |
|--------------|-------|---------|
| `operations-started` | `exercise.started` | `Open` both factions' operations over the map |
| `operations-directives` | `command.directive.issued` | `Assign` the targets a faction's directives name, with their rules |
| `operations-observed` | `exercise.round.observed` | `Maneuver`: record the elements and issue the next round's orders |
| `operations-concluded` | `exercise.concluded` | `Close` the exercise's operations |

Every command claims its event through the inbox, so a redelivery changes nothing. An input for
an operation not open yet, as when a round's observation is handled before its start, is
redelivered after 250ms rather than refused.

The service emits `operations.orders.issued`, whose subject is the exercise's ID:
`{exercise, faction, round, orders: [{element, steps: [{sector, x, y}], retreat?, pursue?}]}`, where
`retreat` appears only on a retreat's order and `pursue` only on a pursuit's. It emits one per
faction per observed round, for the round after it, even with no orders, and none past the round
limit. When a directive changes the plan for orders already issued, it emits them again for the
same round, and exercise keeps the last it records. A directive's payload is
`{exercise, faction, round, sequence, directives: [{element, rule, contact, target}]}`, where
`sequence` numbers a faction's directives in the order command decided them, from 1. The service
reads each directive's rule and target, and keeps them together: a null target holds the element
and drops its rule. It skips a directive whose sequence is no higher than the last it applied,
so a redelivery or a late one behind a newer directive changes nothing, even of the same round.

Each ready element steps toward its target along a shortest path, found by breadth-first search
over open cells and gates (`domain/operations/route`), up to its moves per round: a squad one, a
scout two. An engaged element stays in its fight, because exercise pins it there, unless its
rule is `retreat` and its target is one step away: then its order is that step, flagged as a
retreat. If its rule is `pursue`, its order has no steps and is flagged as a pursuit: it stays in
its cell and fires on an enemy that retreats from it. Any other engaged element gets no order. A
recovering element stays. No two of a faction's elements end a round on one cell, except a cell
where one of its engaged elements stands, which reinforcements may join.

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
with `OPERATIONS_*` environment variables applied last. `operations-serve` sets
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
