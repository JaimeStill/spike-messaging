// Package app is courier's composition root, laid out one file per layer:
// config.go binds the persistent flags, infrastructure.go builds the broker
// the flags name, the outbox scenario's store on the Postgres server they
// name, and the request scenario's exchange on the NATS server, scenarios.go
// mounts the narrated scenarios and their listing, and commands.go composes
// the mounts. app.go holds the application itself: New
// assembles the layers without I/O, and Run executes the command tree and
// turns its error into the exit code.
//
// It is the only package that names a provider: every package beneath it
// works against messaging.Broker, the engine-agnostic outbox, and the
// scenario package's request exchange. The NATS provider and nats.go, and the
// Postgres engine, its SQL dialect, and the pgx driver, are imported here
// alone; the request exchange, a native use beyond the broker's standard
// tier, is built here on nats.go directly.
package app
