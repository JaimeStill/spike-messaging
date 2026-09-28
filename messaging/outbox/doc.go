// Package outbox writes events through a transactional outbox and publishes
// them with a relay, on any SQL engine that supplies the statements.
//
// An [Outbox] is built with [New] over an [Engine]: every statement the
// outbox runs, compiled for one engine by that engine's module, such as
// messaging/postgres. The engine's module also ships the tables as a
// migration set. This package holds no SQL and imports no driver.
//
// [Outbox.Sink] is the [event.Sink] a composition root builds a service's
// [event.Recorder] over. It inserts each event a command raises in the
// command's own transaction, so an event exists exactly when the state it
// reports was committed. Nothing is published there: the [Relay] publishes
// the committed rows afterward, each inside a transaction of its own that
// locks the row and marks it published once the broker holds the event, so a
// stop between the commit and the publish loses no event. A consumer's guard
// against handling a redelivered event twice is the inbox, in
// messaging/inbox.
package outbox
