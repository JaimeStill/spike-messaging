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

## Composition

`internal/app` builds one layer per file, and the lifecycle coordinator starts them in stage
order:

| Stage | Service |
|-------|---------|
| 0 | `database`, and `broker`, which connects and provisions the shared stream at start |
| 1 | `schema`: go-database's admin service migrates the `messaging` set |
| 2 | `messaging` verifies its statements |
| 3 | `relay`, which publishes the outbox |
| root | `server` |

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
integration tier across every module. For this service, the integration tier runs the built
service against the compose stack, on a scratch database and stream of its own.
