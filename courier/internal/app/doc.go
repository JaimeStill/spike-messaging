// Package app is courier's composition root, laid out one file per layer:
// config.go binds the persistent flags, infrastructure.go builds the broker
// the flags name and the outbox scenario's store on the Postgres server they
// name, scenarios.go mounts the narrated scenarios and their
// listing, and commands.go composes the mounts. app.go holds the application
// itself: New assembles the layers without I/O, and Run executes the command
// tree and turns its error into the exit code.
//
// It is the only package that names a provider: every package beneath it
// works against messaging.Broker and the engine-agnostic outbox. The
// Postgres engine, its SQL dialect, and the pgx driver are imported here
// alone.
package app
