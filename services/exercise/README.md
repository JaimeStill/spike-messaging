# exercise

The exercise service runs the world of the spike's exercise (`context/exercise.md`), a
two-faction exercise of sector dominance, from creation to conclusion. The service resolves one
round per interval, records the orders the operations service issues, and reports each round as
events. It was generated from
[go-web-sdk-template](https://github.com/standards-lab/go-web-sdk-template) `template/v0.9.0`.

## Running

The service runs on the host against the repository's compose stack: Postgres, which holds the
`exercise` database, and NATS with JetStream. From the repository root:

```sh
mise run up               # start Postgres and NATS
mise run exercise-serve   # run the service on 127.0.0.1:8081
```

`mise run reset` recreates the stack's volumes. `compose/postgres/init.sql` creates the service
databases when the volume is first initialized. The service migrates only its own schema, at
startup.

`fixtures/skirmish.json` is the demonstration: 30 rounds of 1s between red and blue, with no
map, elements, or seed, so the service lays out the skirmish from a seed it draws. With all four
exercise services running, `mise run demo-theater` creates and starts it, and narrates it through
courier's `theater` scenario; `SEED=7 mise run demo-theater` replays seed 7. `mise run
demo-theater-check` then reconciles every assessment of the run against this service's history,
the umpire's record, under the suppression rules, through courier's `theater-check` scenario, and
reports what each faction believes of each objective at the end against the truth.

## API

Mounted under `/api/exercises`:

| Route | Action |
|-------|--------|
| `POST /` | Create an exercise: its name, its two factions, a round interval, a round limit, and optionally a seed, and a map with its elements |
| `GET /{id}` | The umpire's view: status, seed, round, the whole state, the verdict, and the rules' constants |
| `GET /{id}/history` | Every round from the start: its state, both factions' observations, its verdict, and its resolution |
| `POST /{id}/start` | Start the exercise; round 0 is observed at once |
| `POST /{id}/pause` · `/resume` | Hold the exercise's rounds, and resume them |
| `POST /{id}/stop` | End the exercise |

A create that gives no seed gets one drawn from [0, 2^31), and the exercise keeps it: every
round's random draws come from the seed and the round, so a seed with the orders the exercise
recorded replays it; the services' orders depend on timing, so a seed alone replays a run only as
far as they arrive alike. Only this service's API gives the seed, since the layout and every
fight follow from it. A create gives a map and its elements together or neither. With neither,
the exercise starts from the skirmish the seed lays out: one 13×13 sector with three to five
objectives, and three squads and two scouts a side on opposite baselines, mirrored so neither side
is favored. With both, the exercise starts from them as given.

An element fields operators, each with health from 1 to 100: a squad up to four, a scout one. Its
strength is the sum of their health, and its status is `ready`, `engaged` in a fight, or
`recovering` from a retreat. An order names an element and the cells it steps through; an engaged
element moves only on an order of one step marked `"retreat": true`. An order marked
`"pursue": true` keeps an engaged element in its fight to fire on an enemy that retreats from it,
and the retreating element fires back; a retreat no one pursues escapes without a shot. Round 0,
the start, resolves nothing, so its resolution in the history is `null`.

The umpire's view carries the rules' constants, so that a client reads the state by them and never
mirrors them: `"rules": {"capture_rounds": 2, "sight": {"squad": 1, "scout": 2}}`. `capture_rounds`
is the rounds in a row a faction must end alone on an objective to take it, and `sight` is the
distance, in cells of its own sector, that each kind of element sees.

`/healthz` and `/readyz` are the probes. Readiness reports the database, the broker, the schema
service, and each reactor.

## Events

Each event is written in the transaction of the command that makes it true, and its subject is
the exercise's ID:

- `exercise.started`: the public settings and the terrain, which is the map without its
  objectives. A faction finds an objective only by seeing it. It carries no seed.
- `exercise.round.resolved`: one per resolved round, before its observations: the umpire's record
  of the round's retreats and the fire they exchanged with their pursuers, its engagements, with
  every element's strength before and after and the operators it lost, the elements destroyed,
  the objectives that changed hands, and each faction's progress toward the objectives it is
  taking. It is the resolution the history records for the round.
  It shows both factions, so it is for an observer of the whole exercise, such as courier's
  theater narration; no service consumes it.
- `exercise.objective.lost`: one per objective taken from the faction holding it, after the
  round's resolution: `{exercise, faction, round, at, holder}`, where `faction` lost the objective
  and `holder` took it. It alerts the loser, though none of its elements sees the objective.
- `exercise.round.observed`: one per faction per round, round 0 at the start.
- `exercise.concluded`: the verdict, or a stop.

The service consumes `operations.orders.issued` through the `exercise-orders` subscription, from the
consumer's creation, so a first boot skips the orders the stream already retains; the start position
is part of the consumer's configuration, so a stream whose durable an earlier version created fails
the bind at startup, and `mise run reset` recreates it. It skips a late order, one for a round
already resolved or an exercise that ended: it records nothing and acknowledges the delivery. It
permanently refuses orders no redelivery could fix, such as an unknown exercise or faction, so the
broker does not redeliver them, and the messaging runtime logs the refusal as `event refused`.

## Composition

`internal/app` builds one layer per file, and the lifecycle coordinator starts them in stage
order:

| Stage | Service |
|-------|---------|
| 0 | `database`, and `broker`, which connects and provisions the shared stream at start |
| 1 | `schema`: go-database's admin service migrates the `messaging` set, then `exercise` |
| 2 | `messaging` and `exercise` verify their statements |
| 3 | `orders`, the reactor that records operations' orders |
| 4 | `relay`, which publishes the outbox |
| root | `server`, and `resolve`, which resolves due rounds |

The drain runs in reverse. The server and the resolver, which commit events, stop first, and the
relay then publishes what they committed while they drained. The broker and database close last.

## Configuration

Configuration is layered: `config.json`, `config.<EXERCISE_ENV>.json`, and the secrets files, with
`EXERCISE_*` environment variables applied last. `exercise-serve` sets `EXERCISE_ENV=local`, so
`config.local.json` points the service at the compose stack. The service adds three blocks to the
template's:

- `database`: go-database's connection block.
- `messaging`: `messaging.Config`, the service's CloudEvents `source` and the relay's
  `relay_poll`.
- `nats`: `nats.Config`, the NATS URL, the connection's name, the shared stream and its prefix,
  and the stream's `max_age`.

## Testing

From the repository root, `mise run test` runs the unit tier and `mise run integration` runs the
integration tier across every module. For this service, the integration tier covers three things:

- the domain's commands, on a scratch database;
- the built service, against the compose stack, on a scratch database and stream of its own;
- an idle exercise that runs to its round limit and ends in a draw.
