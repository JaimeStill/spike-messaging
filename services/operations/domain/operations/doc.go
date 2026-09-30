// Package operations is the operations domain. It maneuvers each faction's
// elements in an exercise toward the targets its directives set. For each
// exercise and faction it stores the public map, the elements as the last
// observation left them, and each element's target and its directive's
// rule. It turns each observed round into the faction's orders for the
// next. The pure route package plans the steps; this package persists their
// inputs and reports the plan as events.
//
// The package's files divide the work:
//
//   - statements/ holds the SQL, one authored file per statement with its
//     tier header.
//   - database.go is the domain's SQL client. It is the only file that
//     imports the query library and the only place that encodes route's
//     values to JSON for their jsonb columns and decodes them back.
//   - events.go declares the domain's event, raises it, and holds the
//     helper every mutating command runs through.
//   - service.go holds the commands and the query.
//   - entities.go defines the domain's types; each command input validates
//     itself.
//
// # Commands
//
// A consuming reactor invokes each command for an event another service
// raised, with the event's data decoded into the command's input. Each
// command takes an optional [Claim]. The reactor binds the claim over its
// inbox, and the command runs it first in its transaction, so a redelivery
// changes nothing.
//
//   - [Service.Open] opens both factions' operations on exercise.started.
//   - [Service.Assign] sets targets and rules on command.directive.issued.
//   - [Service.Maneuver] records the faction's elements on
//     exercise.round.observed and raises [OrdersIssued] for the next round.
//   - [Service.Close] closes the exercise's operations on
//     exercise.concluded.
//
// A command refuses an input that no redelivery could fix, such as one that
// fails validation, with an error that [event.IsPermanent] reports. An input
// for an operation not open yet fails with [ErrNotOpen], which is not
// permanent: the events arrive on separate subscriptions, so a round's
// observation can be handled before the start that opens its operation, and
// the broker redelivers it. Assign and Maneuver lock their faction's row, so
// replicas act on one faction's inputs one at a time. Both skip any input
// for a closed operation. Assign skips a directive whose sequence is no
// higher than the last it applied, and Maneuver skips an observation of a
// round no later than the last it acted on.
package operations
