// Package nats is the NATS JetStream messaging provider.
//
// A [Broker] publishes to one stream and subscribes durable pull consumers
// on it. It is a lifecycle component at the lowest stage. [New] checks its
// [Config] and does no I/O, so a composition root can build it cold;
// [Broker.Start] connects to Config.URL, reconnecting without limit once it
// is up, and provisions the stream idempotently, so every replica converges
// on the same configuration. [Broker.Shutdown] drains the connection once
// the reactors above it have drained, and [Broker.Ready] reports it: an
// outage makes the broker not ready rather than ending the process. A call
// that reaches the broker before Start fails with [ErrNotStarted], which the
// coordinator's stage order prevents.
//
// Config is also the service's nats configuration block: its JSON form,
// [Config.Merge], and [Config.Finalize] follow go-core's config conventions.
//
// An event is published in binary content mode to the subject
// Prefix.<type>, so a type must pass [messaging.CheckType], with its source
// and id as Nats-Msg-Id: the stream's deduplication window drops a repeat,
// such as an outbox relay's republish. A header value is sent verbatim, and one that
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
// before it redelivers on a fast enough link, and an outcome that misses the
// deadline is dropped unsent. nil acknowledges and waits for the server to confirm it, so a
// drained handler's acknowledgement holds; an error naks with the
// subscription's RetryDelay; an error marked by [event.Permanent], or a
// message that cannot decode, terminates.
//
// [Broker.Conn] is the native handle, for uses beyond the standard tier such
// as request and reply. It belongs in the composition root.
package nats
