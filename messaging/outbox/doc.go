// Package outbox emits events through a transactional outbox and publishes
// them with a relay, on any SQL engine that supplies the statements.
//
// An [Outbox] is built with [New] over an [Engine]: every statement the
// outbox runs, compiled for one engine by that engine's module, such as
// messaging/outbox/postgres. The engine's module also ships the tables as a
// migration set. This package holds no SQL and imports no driver.
//
// [Outbox.Emitter] is the [event.Emitter] a composition root injects into a
// domain. It inserts each event in the domain's own transaction, so an event
// exists exactly when the state it reports was committed. Nothing is
// published there: the [Relay] publishes the committed rows afterward, each
// inside a transaction of its own that locks the row and marks it published
// once the broker holds the event, so a stop between the commit and the
// publish loses no event. [Outbox.Claim] is the inbox, a handler's guard against
// handling a redelivered event twice.
package outbox
