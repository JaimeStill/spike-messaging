// Package nats is the NATS JetStream messaging provider.
//
// A [Broker] publishes to one stream and subscribes durable pull consumers
// on it. The composition root connects to NATS and hands the connection to
// [New], which provisions the stream idempotently, so every replica
// converges on the same configuration. The broker then owns the connection:
// it is a lifecycle component at the lowest stage, whose [Broker.Shutdown]
// drains the connection once the reactors above it have drained, and whose
// [Broker.Ready] reports it.
//
// An event is published in binary content mode to the subject
// Prefix.<type>, so a type must pass [messaging.CheckType], with its id as
// Nats-Msg-Id: the stream's deduplication window drops a repeat, such as an
// outbox relay's republish. A header value is sent verbatim, and one that
// NATS cannot carry, holding a CR or LF, fails Publish.
//
// A subscription's source binds its consumer when it starts receiving: a
// durable named for the subscription, filtered to its types, delivering the
// stream from its beginning. A binding whose configuration differs from the
// consumer's fails Receive. The source pulls one message at a time, and the
// next only once the handler's outcome is settled, so members of a Name
// share the work. The handler's deadline is the delivery's receipt plus the
// subscription's AckWait; the consumer's own AckWait is longer by
// [AckMargin], so an outcome settled before the deadline reaches the server
// before it redelivers, and an outcome that misses the deadline is dropped
// unsent. nil acknowledges and waits for the server to confirm it, so a
// drained handler's acknowledgement holds; an error naks with the
// subscription's RetryDelay; an error marked by [event.Permanent], or a
// message that cannot decode, terminates.
//
// [Broker.Conn] is the native handle, for uses beyond the standard tier such
// as request and reply. It belongs in the composition root.
package nats
