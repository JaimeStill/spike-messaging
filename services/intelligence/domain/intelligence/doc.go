// Package intelligence is the intelligence domain. It keeps what each
// faction in an exercise knows: for each exercise and faction it stores a
// picture, the faction's own elements, the enemy contacts it knows of, and
// every objective's last-seen holder, and it fuses each observed round into
// that picture and reports the result as the faction's assessment. The pure
// fusion package fuses and enforces the suppression rules; this package
// persists the picture and reports it as events.
//
// The package's files divide the work:
//
//   - statements/ holds the SQL, one authored file per statement with its
//     tier header.
//   - database.go is the domain's SQL client. It is the only file that
//     imports the query library and the only place that encodes fusion's
//     picture to JSON for its jsonb column and decodes it back.
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
//   - [Service.Open] opens both factions' assessments on exercise.started.
//   - [Service.Observe] fuses a faction's observation on
//     exercise.round.observed and raises [AssessmentIssued].
//   - [Service.Close] closes the exercise's assessments on
//     exercise.concluded.
//
// A command refuses an input that no redelivery could fix, such as one that
// fails validation, with an error that [event.IsPermanent] reports. An input
// for an assessment not open yet fails with [ErrNotOpen], which is not
// permanent: the events arrive on separate subscriptions, so a round's
// observation can be handled before the start that opens its assessment,
// and the broker redelivers it. Each command locks its faction's row, so
// replicas act on one faction's observations one at a time. Observe skips
// an observation of a round the assessment already covers, and one of a
// closed assessment past the round its exercise concluded after, so the
// final round is assessed even when its conclusion is handled first.
package intelligence
