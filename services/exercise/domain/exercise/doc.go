// Package exercise is the exercise domain layer: the world of a
// two-faction exercise of sector dominance, its life from creation to
// conclusion, the orders the factions record for each round, and the
// resolution of each round when it is due. The rules a round resolves by
// are the rules package's, which is pure; this package persists what they
// produce and reports it as events.
//
// The layer's SQL is the statements/ directory, one authored file per
// statement with its tier header; database.go is the domain's SQL client,
// the sole importer of the query library: it compiles the directory once
// against the service's pattern catalog, binds each statement to a typed
// handle, exposes the operations as the store's methods, and is the one
// place the rules' values become JSON for their jsonb columns and back.
// events.go is the one place the domain's events are declared and raised,
// with the command helper every mutating command runs through. service.go
// holds the commands and the queries. entities.go owns the shapes and
// their rules: each command validates itself.
//
// # Events
//
// A domain raises, and the recorder emits. Each command runs as one
// transaction through the service's command helper, which hands its body
// an event queue and has the recorder write the queued events into the
// same transaction when the body succeeds, so an event is emitted only with
// the state it reports, and a command that fails emits nothing. Every
// event's subject is the exercise's ID. Start raises [Started] and round
// 0's [RoundObserved], one per faction; each resolved round raises
// [RoundObserved] per faction; the round that ends the exercise, or a stop
// of one that had started, raises [Concluded].
//
// # Time
//
// Every time the layer keeps comes from the database's clock, never the
// process's, so replicas agree on what is due: a running exercise's next
// round is due one round interval after the transaction that started,
// resumed, or advanced it, and [Service.ResolveDue] resolves the exercises
// whose next round the database's clock has reached. Each is locked with
// SKIP LOCKED in a transaction of its own, so replicas resolving at once
// share the work and never resolve one round twice.
//
// # Orders
//
// [Service.RecordOrders] is the command a reactor's adapter invokes for
// the orders a faction issued. It takes an optional [Claim], which the
// adapter binds over its inbox, and runs it first in the command's
// transaction, so a redelivery changes nothing. It reads the exercise under
// a shared lock, so it waits on a resolution in flight, and refuses what no
// redelivery could fix, an order for a round already resolved among it,
// with an error [event.IsPermanent] reports. A faction's orders for a round
// replace any it recorded before; at resolution, an order for an element of
// the other faction is ignored.
package exercise
