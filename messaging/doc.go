// Package messaging is the standard tier's broker operations: publishing an
// event, and subscribing to events as a reactor source.
//
// A [Broker] is what a composition root builds reactors from. A
// [Subscription] names a durable consumer: every source subscribed under
// one Name shares that consumer's position and splits its work, so replicas
// of a service form a delivery group, and each distinct Name receives every
// event. The handler's return is the delivery's outcome, so a reactor
// acknowledges, redelivers, or terminates without a broker type in sight.
//
// The package imports no broker. A provider, such as messaging/memory,
// implements [Broker], and messaging/messagingtest holds the conformance
// suite every provider passes.
package messaging
