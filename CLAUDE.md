# spike-messaging

A spike for the Standards Lab reference architecture. It tests whether one broker-agnostic event
and reactor contract can run a service's reactors on NATS JetStream and on an in-memory provider.
The repository is managed with the marathon workflow; start from `context/README.md`.

- **Modules:** a committed `go.work` layers the modules, as spike-harness-driver does. The root
  module, `github.com/JaimeStill/spike-messaging`, holds the libraries. Each library package sits in a
  directory named for its intended home (`core/…` for go-core, `messaging/…` for go-messaging), so
  promoting a package is a move and the import graph tests the split. `messaging/outbox/postgres` is the
  outbox's Postgres engine, a module of its own so that pgx stays out of the root, and `courier`
  is the application's own module. A module's `go.mod` has no `require` for a workspace sibling, since `go.work` resolves
  it, so `go mod tidy` runs only at the root and other `go.mod` files are edited by hand. The mise
  tasks name every module through `MODULES`.
- **Dependencies:** published versions only, never a `replace` directive.
- **References:** `references.toml` lists the repositories this spike reads, and the gitignored
  `references.local.toml` maps them to local checkouts. The spike reads them and never writes to
  them.
