# command

The command service decides for each faction of the spike's exercise (`context/exercise.md`): it
turns each faction's assessments into directives. It was generated from
[go-web-sdk-template](https://github.com/standards-lab/go-web-sdk-template) `template/v0.9.0`.

## Running

The service runs on the host against the repository's compose stack: Postgres, which holds the
`command` database, and NATS with JetStream. From the repository root:

```sh
mise run up                 # start Postgres and NATS
mise run command-serve      # run the service on 127.0.0.1:8083
```

## API

Mounted under `/api/command`:

| Route | Action |
|-------|--------|
| `GET /{exercise}` | Each faction's direction: its status, the last round it decided on, and the decision standing for each live element, with its rule, its target, and for a retreat, a pursue, an engage, or a reinforce its contact |

`/healthz` and `/readyz` are the probes. Readiness reports the database, the broker, the schema
service, and each reactor. The commands have no route: their inputs arrive as events.

## Events

The service consumes three event types, each through a subscription of its own, and decodes each
payload into its command's input, its own reading of the payload:

| Subscription | Event | Command |
|--------------|-------|---------|
| `command-started` | `exercise.started` | `Open` both factions' directions over the map |
| `command-assessed` | `intelligence.assessment.issued` | `Decide`: decide on the faction's assessment |
| `command-concluded` | `exercise.concluded` | `Close` the exercise's directions |

Every command claims its event through the inbox, so a redelivery changes nothing. An input for
a direction not open yet, as when an assessment is handled before its start, is redelivered after
250ms rather than refused.

`Decide` skips an assessment of a round earlier than the one it last decided on, and decides
again on an assessment of that round: intelligence revises one when the faction loses an
objective.

`Decide` applies eight rules to each live element, in order, by path distance over open cells and
gates. An element is engaged when intelligence reports it so and an enemy was seen in its cell this
round. Objectives are hidden: an assessment lists only the objectives its faction has discovered,
and the cells its elements have ever had in sight; any other open cell is unexplored.

1. **Retreat.** An engaged element steps out of its fight when it is a scout, or when its
   faction's strength in the cell is below two thirds of the enemy's there. It steps to the
   adjacent open cell free of the enemy that lies farthest from the nearest contact outside the
   fight, ties going up, right, down, then left. Only the enemy elements that pursue fire on a
   retreat.
2. **Pursue or engage in place.** An engaged element that doesn't retreat, or has nowhere to go,
   holds its fight: its target is its own cell. It pursues when its faction's strength in the
   cell is at least the enemy's, and so fires on an enemy that retreats; otherwise it engages.
3. **Reinforce.** A ready or recovering squad heads for the nearest fight of its faction within 4
   steps. Any number of squads may reinforce one fight.
4. **Engage.** A squad heads for the nearest known contact weaker than itself within 3 steps, at
   the cell it was last seen in.
5. **Secure.** Otherwise the element heads for the nearest reachable discovered objective that its
   faction isn't known to hold and that no other element is heading for. A scout secures only
   once no unexplored cell is in its reach. An element keeps the objective it was securing while
   that objective is still one to secure.
6. **Search.** Otherwise the element heads for the nearest reachable unexplored cell that no other
   element is searching, preferring one more than two cells from every cell already taken, so the
   searchers spread. Scouts pick before squads, and an element keeps the cell it was searching
   while that cell is still unexplored.
7. **Rescout.** Otherwise the element heads for the discovered objective its faction isn't known
   to hold that was seen longest ago, ties going to the nearest, that no other element is
   rescouting and that it doesn't stand on, though another element may be securing it. An idle
   element so refreshes a belief that may be stale, and keeps the objective it was rescouting
   while that objective is still one to secure.
8. **Hold.** Otherwise it stands where it is.

The service emits `command.directive.issued`, whose subject is the exercise's ID:
`{exercise, faction, round, directives: [{element, rule, contact, target}]}`, where `rule` is
`retreat`, `pursue`, `engage`, `reinforce`, `secure`, `search`, `rescout`, or `hold`, and a hold's
target is null. A retreat, a pursue, an engage, and a reinforce have a contact: the enemy the
element leaves, fights, or heads for, the strongest in the cell when a fight holds several. The
service emits the event only when a decision changes an element's target or rule. The event lists
every live element, so it carries the faction's whole target state. The operations service reads
each directive's element, rule, and target.

The messaging runtime logs the traffic: the relay logs each event it publishes, and each consumer
logs each delivery with its outcome.

## Composition

`internal/app` builds one layer per file, and the lifecycle coordinator starts them in stage
order:

| Stage | Service |
|-------|---------|
| 0 | `database`, and `broker`, which connects and provisions the shared stream at start |
| 1 | `schema`: go-database's admin service migrates the `messaging` set, then `command` |
| 2 | `messaging` and `command` verify their statements |
| 3 | `relay`, which publishes the outbox |
| 4 | `started`, `assessed`, and `concluded`, the consumers |
| root | `server` |

The drain runs in reverse. The consumers raise the service's directives, so they sit above the
relay: the drain stops them first, and the relay's last pass publishes what they committed. The
broker and database close last.

## Configuration

Configuration is layered: `config.json`, `config.<COMMAND_ENV>.json`, and the secrets files,
with `COMMAND_*` environment variables applied last. `command-serve` sets `COMMAND_ENV=local`, so
`config.local.json` points the service at the compose stack. The service adds three blocks to the
template's:

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
