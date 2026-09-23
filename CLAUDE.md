# spike-messaging

A spike for the Standards Lab reference architecture. It tests whether one broker-agnostic event
and reactor contract can run a service's reactors on NATS JetStream and on an in-memory provider.
The repository is managed with the marathon workflow; start from `context/README.md`.

- **Module:** one Go module, `github.com/JaimeStill/spike-messaging`. Each package sits in a
  directory named for its intended home (`core/…` for go-core, `messaging/…` for go-messaging), so
  promoting a package is a move and the import graph tests the split.
- **Dependencies:** published versions only, never a `replace` directive.
- **References:** `references.toml` lists the repositories this spike reads, and the gitignored
  `references.local.toml` maps them to local checkouts. The spike reads them and never writes to
  them.
