// Package operations is the operations domain. It maneuvers each faction's
// elements in an exercise toward the targets its directives set. For each
// exercise and faction it stores the public map, the elements as the last
// observation left them, and each element's target, and it turns each
// observed round into the faction's orders for the next. The pure route
// package plans the steps; this package persists their inputs and reports
// the plan as events.
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
// A reactor's adapter invokes each command for an event another service
// raised. Each command takes an optional [Claim]. The adapter binds the
// claim over its inbox, and the command runs it first in its transaction, so
// a redelivery changes nothing.
//
//   - [Service.Open] opens both factions' operations on exercise.started.
//   - [Service.Assign] sets targets on command.directive.issued.
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
// the broker redelivers it. Each command locks its faction's row, so
// replicas act on one faction's inputs one at a time. Each command skips an
// input from a round earlier than the last the operation acted on, and any
// input for a closed operation.
package operations
