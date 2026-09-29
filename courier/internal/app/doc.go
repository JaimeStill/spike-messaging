// Package app is courier's composition root, one file per layer.
// config.go binds the persistent flags. infrastructure.go builds the broker
// the flags name, the outbox scenario's store on the Postgres server they
// name, the request scenario's exchange on the NATS server, and the
// directives scenario's broker on the exercise services' stream.
// scenarios.go mounts the narrated scenarios and their listing, and
// commands.go composes the mounts. app.go holds the application itself: New
// assembles the layers without I/O, and Run executes the command tree and
// turns its error into the exit code.
//
// It is the only package that names a provider: every package beneath it
// works against messaging.Broker, the engine-agnostic outbox, and the
// scenario package's request exchange. The NATS provider, nats.go, the
// Postgres engine, its SQL dialect, and the pgx driver are imported here
// alone. The request exchange, a native use beyond the broker's standard
// tier, is built here on the nats broker's handle.
package app
