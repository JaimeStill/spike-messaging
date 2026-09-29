// Package operations is the operations domain: it maneuvers each faction's
// elements in an exercise toward the targets its directives set. It keeps,
// for each exercise and faction, the public map, the faction's elements as
// its last observation left them, and each element's target, and turns each
// observed round into the faction's orders for the next. The route package,
// which is pure, plans the steps; this package persists what it plans from
// and reports the plan as events.
//
// The package's files divide the work:
//
//   - statements/ holds the SQL, one authored file per statement with its
//     tier header.
//   - database.go is the domain's SQL client and the package's only importer
//     of the query library, and the only place route's values are encoded to
//     JSON for their jsonb columns and decoded back.
//   - events.go declares the domain's event, raises it, and holds the
//     command helper every mutating command runs through.
//   - service.go holds the commands and the query.
//   - entities.go defines the domain's types; each command input validates
//     itself.
//
// # Commands
//
// Every command is one a reactor's adapter invokes for an event another
// service raised, and each takes an optional [Claim], which the adapter
// binds over its inbox and the command runs first in its transaction, so a
// redelivery changes nothing.
//
//   - [Service.Open] opens both factions' operations on exercise.started.
//   - [Service.Assign] sets targets on command.directive.issued.
//   - [Service.Maneuver] records the faction's elements on
//     exercise.round.observed and raises [OrdersIssued] for the next round.
//   - [Service.Close] closes the exercise's operations on
//     exercise.concluded.
//
// An input that no redelivery could fix, such as one that fails its
// validation, is refused with an error that [event.IsPermanent] reports. An
// input for an operation not open yet fails with [ErrNotOpen], which is not
// permanent: the events arrive on separate subscriptions, so a round's
// observation can be handled before the start that opens its operation,
// and the broker redelivers it. Each command locks its faction's row, so
// replicas act on one faction's inputs one at a time, and each skips an
// input from a round earlier than the last the operation acted on, and any
// for a closed operation.
package operations
