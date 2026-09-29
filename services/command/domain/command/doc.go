// Package command is the command domain. It decides what each faction in
// an exercise directs its elements to do: for each exercise and faction it
// stores the public map and the decision standing for each live element,
// decides again on each of the faction's assessments, and reports a
// changed decision as the faction's directives. The pure decide package
// applies the rules; this package persists the decisions and reports them
// as events.
//
// The package's files divide the work:
//
//   - statements/ holds the SQL, one authored file per statement with its
//     tier header.
//   - database.go is the domain's SQL client. It is the only file that
//     imports the query library and the only place that encodes decide's
//     map and decisions to JSON for their jsonb columns and decodes them
//     back.
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
//   - [Service.Open] opens both factions' directions on exercise.started.
//   - [Service.Decide] decides on a faction's assessment on
//     intelligence.assessment.issued, and raises [DirectiveIssued] when an
//     element's target changes.
//   - [Service.Close] closes the exercise's directions on
//     exercise.concluded.
//
// A command refuses an input that no redelivery could fix, such as one that
// fails validation, with an error that [event.IsPermanent] reports. An input
// for a direction not open yet fails with [ErrNotOpen], which is not
// permanent: the events arrive on separate subscriptions, so a round's
// assessment can be handled before the start that opens its direction, and
// the broker redelivers it. Each command locks its faction's row, so
// replicas act on one faction's assessments one at a time. Decide skips an
// assessment of a round the direction already decided on, and one of a
// closed direction past the round its exercise concluded after, so the
// final round is decided on even when its conclusion is handled first.
//
// A directive lists every live element, not only those whose target
// changed. operations skips an input from a round earlier than the last it
// acted on, so a directive it skips would otherwise lose its change for
// good; the next directive it takes carries the whole target state.
package command
