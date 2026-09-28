# Service

Generated from [go-web-sdk-template](https://github.com/standards-lab/go-web-sdk-template).

## After generation

Three steps localize the service's identity:

1. Rename `envPrefix` in `internal/config/config.go` — the single constant every `EXERCISE_*`
   environment-variable name derives from.
2. Rename the `EXERCISE_ENV` key in `mise.toml`'s `[env]` block to follow the prefix.
3. Rewrite this README for the service.

Licensing and release automation are yours to define; CI arrives working (`.github/workflows/ci.yml`).

## Getting started

[mise](https://mise.jdx.dev/) provisions the toolchain and runs the tasks:

```sh
mise trust && mise install
mise run serve
```

The service logs `server ready` on `localhost:8080` (the `local` overlay binds loopback and runs
debug logging). From a second shell:

```sh
curl localhost:8080/healthz   # 200 {"status":"ok"}
curl localhost:8080/readyz    # 200 {"status":"ready","checks":[...]}
```

Ctrl-C drains in-flight requests and exits with `server stopped`.

## Tasks

Each task wraps a plain command, so the repository works without mise:

| Task | Command | What it does |
|------|---------|--------------|
| `mise run vet` | `go vet -tags integration ./...` | Compile-check and vet, the integration suite included |
| `mise run serve` | `go run ./cmd/server` | Run the service locally |
| `mise run test` | `go test -race ./...` | Run the unit tier |
| `mise run integration` | `go test -race -count=1 -tags integration ./integration/` | Run the integration tier against the built service |
| `mise run fmt` | `gofmt -w .` | Format the source |
| `mise run tidy` | `go mod tidy` | Reconcile module requirements |
| `mise run lint` | `golangci-lint run --build-tags integration ./...` | Lint, the integration suite included |

## Testing

Two tiers. The unit tier is hermetic and runs on every pull request: `go test -race ./...`,
touching no service, network, or disk. The integration tier is the `integration` package: an
untagged harness that builds `cmd/server` once, runs it as a subprocess configured by `EXERCISE_*`
variables on a port it reserved, and drives it through its HTTP surface, over the toolkit the
SDKs ship for the purpose (go-core's `process/processtest`, go-web-sdk's `webtest`). Under the
`integration` build tag, the suite asserts the service's behavior through its API. It runs on
merge to main and on demand, below the per-PR rate by design.

The template's suite asserts the baseline: boot, both probes, and the drain. As the build points
fill in, each capability adds its cases beside them, and the first backing service brings its
compose stack: the task then boots that stack as an isolated compose project on its own port,
runs the suite, and tears it down with its volume on exit, so a developer's stack and data are
never touched. The reference service shows the shape.

## Configuration

Configuration layers in a fixed precedence, later sources winning:

1. `config.json` — the base file, every knob at its default.
2. `config.<EXERCISE_ENV>.json` — the environment overlay; `mise.toml` sets `EXERCISE_ENV=local`, which
   activates the committed `config.local.json` (loopback host, debug logging).
3. `secrets.json`, `secrets.<EXERCISE_ENV>.json` — gitignored secret layers.
4. `EXERCISE_*` environment variables — the final override:
   - `EXERCISE_LOG_LEVEL`, `EXERCISE_LOG_FORMAT`
   - `EXERCISE_SERVER_HOST`, `EXERCISE_SERVER_PORT`, and the four server timeout variables
   - `EXERCISE_READS_DEFAULT_SIZE`, `EXERCISE_READS_MAX_SIZE`
   - `EXERCISE_SHUTDOWN_TIMEOUT`

Every file is optional — a deployment can run on the base file and environment variables alone,
or on environment variables only.

## Building out the service

The composition root, `internal/app`, is laid out as one file per layer of the architecture,
and those files are the build points:

- `infrastructure.go` for the services the application composes on
- `admin.go` for the administrative services
- `domain.go` for the domain services
- `reactors.go` for the event-driven entry points
- `middleware.go` for the router-level middleware

Each layer file constructs its layer and owns its mount; `routes.go` lists the mounts and does
nothing else. `cmd/server` is the entrypoint alone and never changes.

A domain service starts from its Entity:

1. Give the Entity its own package under `domain/`.
2. Expose its Queries and Commands as the domain service's methods.
3. Build the layer's route group in its handler.
4. Construct the service and mount the group in `domain.go`.

The constructor draws what it uses from the `Infrastructure` fields, and the handler is handed
its policy from the config root at the construction site (`cfg.Reads.Limits()` for a collection
read).

An infrastructure service (a database pool, a storage client, an auth client) is a field on
`Infrastructure` plus its construction in `newInfrastructure` (`internal/app/infrastructure.go`):
assign the field, then declare the lifecycle on the coordinator —

```go
i.DB = db
lc.Add(lifecycle.Service{Name: "database", Stage: 0, Start: db.Start, Shutdown: db.Shutdown, Check: db})
```

Numbered stages start in ascending order ahead of the server's root stage and drain after it,
so in-flight requests complete before their infrastructure closes. A service declared this way
cannot be missing from the probe or the drain, and a field that does not exist fails the build
at its access.

An admin service administers one infrastructure service over the mechanisms its library
provides, and is a field on `Admin` constructed in `newAdmin` (`internal/app/admin.go`) with its
route group mounted under `/admin` in `mountAdmin`. It registers on the coordinator when it
owns a lifecycle stage, ahead of the domains that depend on the state it corrects. The template
serves the empty `/admin` group on the API listener; before the first admin service is mounted,
settle the mount's isolation — its own listener, authentication, and audit — because an
administrative surface on the public port is exposed the moment it serves a route.

A reactor owns a transport connection and runs for the process lifetime: a field on `Reactors`
constructed in `newReactors` (`internal/app/reactors.go`) and registered on the coordinator the
same as an infrastructure service, dispatching each occurrence to a domain service call.

Middleware that applies to every route stacks in `middleware` (`internal/app/middleware.go`),
outermost first; middleware scoped to one domain service belongs on its route group.

Configuration grows by adding fields to `Config` in `internal/config/config.go` and delegating
to their `Merge` and `Finalize` in the existing shape; the `reads` block is the model for a
service-owned block. The service keeps pace with its SDKs by updating its `go-core` and
`go-web-sdk` pins and applying whatever adjustments the release notes call for.
