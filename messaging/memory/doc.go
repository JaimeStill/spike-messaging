// Package memory is the in-memory messaging provider: the conformance double
// for the standard tier and a test double for a service.
//
// A [Broker] keeps one append-only log of encoded events, as a JetStream
// stream would, and a durable consumer for each subscription Name that
// starts at the log's beginning. Publish encodes an event to binary content
// mode and each delivery decodes it, so the codec a real binding uses is
// exercised on every message. Nothing persists beyond the process.
//
// The package reads in three layers: broker.go holds the log and the
// consumers, consumer.go is each consumer's delivery state machine, and
// source.go is the member loop a reactor runs.
package memory
