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
mise run intelligence:serve   # run the service on 127.0.0.1:8082
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

Configuration is layered: `config.json`, `config.<INTELLIGENCE_ENV>.json`, and the secrets files,
with `INTELLIGENCE_*` environment variables applied last. `intelligence:serve` sets
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
integration tier across every module. For this service, the integration tier runs the built
service against the compose stack, on a scratch database and stream of its own.
