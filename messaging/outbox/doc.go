// Package outbox emits events through a transactional outbox on Postgres.
//
// [Emitter] is the [event.Emitter] a composition root injects into a domain.
// It inserts each event into the messaging_outbox table in the domain's own
// transaction, so an event exists exactly when the state it reports was
// committed. Nothing is published there: a relay publishes the committed
// rows afterward, outside any transaction, so a stop between the commit and
// the publish loses no event.
//
// The tables ship as a migration set, [Migrations], which a consumer declares
// ahead of its own. Every object the set creates is prefixed messaging_, and
// its history is recorded in [Table].
package outbox
