# exercise

The world of the spike's exercise (`context/exercise.md`): a two-faction exercise of sector
dominance, from creation to conclusion. The service resolves one round per interval, records the
orders the operations service issues, and reports each round as events. It was generated from
[go-web-sdk-template](https://github.com/standards-lab/go-web-sdk-template) `template/v0.9.0`.

## Running

The service runs on the host against the repository's compose stack: Postgres, which holds the
`exercise` database, and NATS with JetStream. From the repository root:

```sh
mise run up               # start Postgres and NATS
mise run exercise:serve   # run the service on 127.0.0.1:8081
```

`mise run reset` recreates the stack's volumes. `compose/postgres/init.sql` creates the service
databases when the volume is first initialized. The service migrates only its own schema, at
startup.

## API

Mounted under `/api/exercises`:

| Route | |
|-------|-|
| `POST /` | Create an exercise: its map, its two factions, their elements, a round interval, and a round limit |
| `GET /{id}` | The umpire's view: status, round, the whole state, and the verdict |
| `GET /{id}/history` | Every resolved round: its state, both factions' observations, and its verdict |
| `POST /{id}/start` | Start the exercise; round 0 is observed at once |
| `POST /{id}/pause` · `/resume` | Hold the clock, and continue it |
| `POST /{id}/stop` | End the exercise |

`/healthz` and `/readyz` are the probes. Readiness reports the database, the broker, the schema
service, and each reactor.

## Events

Each event is written in the transaction of the command that makes it true, and its subject is
the exercise's ID:

- `exercise.started`: the public settings, including the map but not the elements.
- `exercise.round.observed`: one per faction per round, round 0 at the start.
- `exercise.concluded`: the verdict, or a stop.

The service consumes `operations.orders.issued` as the `exercise-orders` subscription. Orders for a
round already resolved are refused permanently.

## Composition

`internal/app` builds one layer per file, and the lifecycle coordinator starts them in stage
order:

| Stage | Service |
|-------|---------|
| 0 | `database`, and `broker`, which connects and provisions the shared stream at start |
| 1 | `schema`: go-database's admin service migrates the `messaging` set, then `exercise` |
| 2 | `messaging` and `exercise` verify their statements |
| 3 | `orders`, the reactor that records operations' orders |
| root | `server`, `relay`, which publishes the outbox, and `resolve`, which resolves due rounds |

The drain runs in reverse, so the reactors that produce work stop before the ones that consume,
and the broker and database close last.

## Configuration

Configuration is layered: `config.json`, `config.<EXERCISE_ENV>.json`, and the secrets files, with
`EXERCISE_*` environment variables applied last. `exercise:serve` sets `EXERCISE_ENV=local`, so
`config.local.json` points the service at the compose stack. The service adds two blocks to the
template's:

- `database`: go-database's connection block.
- `messaging`: the NATS URL, the shared stream and its prefix, the stream's `max_age`, the
  service's CloudEvents `source`, and the relay's `relay_poll`.

## Testing

From the repository root, `mise run test` runs the unit tier and `mise run integration` runs the
integration tier across every module. For this service, the integration tier covers three things:

- the domain's commands, on a scratch database;
- the built service, against the compose stack on a scratch stream of its own;
- an idle exercise that runs to its round limit as a draw.
