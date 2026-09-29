// Package exercise is the exercise domain: the world of a two-faction
// exercise of sector dominance. It creates an exercise, moves it through its
// life from start to conclusion, records the orders each faction issues for
// a round, and resolves each round when it is due. The rules package, which
// is pure, decides how a round resolves; this package persists what the
// rules produce and reports it as events.
//
// The package's files divide the work:
//
//   - statements/ holds the SQL, one authored file per statement with its
//     tier header.
//   - database.go is the domain's SQL client and the package's only importer
//     of the query library. It compiles statements/ once against the
//     service's pattern catalog, binds each statement to a typed handle, and
//     exposes the operations as methods of the store. It is also the only
//     place where the rules' values are encoded to JSON for their jsonb
//     columns and decoded back.
//   - events.go declares the domain's events, raises them, and holds the
//     command helper that every mutating command runs through.
//   - service.go holds the commands and the queries.
//   - entities.go defines the domain's types and their rules; each command
//     input validates itself.
//
// # Events
//
// A domain raises, and the recorder emits. Each command runs as one
// transaction through the service's command helper. The helper gives the
// command's body an event queue, and when the body succeeds, the recorder
// writes the queued events into the same transaction. An event is therefore
// emitted only with the state it reports, and a command that fails emits
// nothing. Every event's subject is the exercise's ID.
//
//   - Start raises [Started], then [RoundObserved] for round 0, one per
//     faction.
//   - Each resolved round raises [RoundObserved], one per faction.
//   - The round that ends the exercise raises [Concluded], and so does a
//     stop of an exercise that had started.
//
// # Time
//
// Every time the package keeps comes from the database's clock, never the
// process's, so replicas agree on what is due. A running exercise's next
// round is due one round interval after the transaction that started,
// resumed, or advanced it. [Service.ResolveDue] resolves the exercises whose
// next round the database's clock has reached. It locks each one with SKIP
// LOCKED in a transaction of its own, so replicas that resolve at the same
// time share the work and never resolve one round twice.
//
// # Orders
//
// [Service.RecordOrders] is the command a reactor's adapter invokes for the
// orders a faction issued. It takes an optional [Claim], which the adapter
// binds over its inbox, and runs the claim first in the command's
// transaction, so a redelivery changes nothing. The command reads the
// exercise under a shared lock, so it waits for a resolution in flight. It
// refuses input that no redelivery could fix, such as an order for a round
// already resolved, with an error that [event.IsPermanent] reports. A
// faction's orders for a round replace any it recorded before. Resolution
// ignores an order for an element of the other faction.
package exercise
