// Package event defines the CloudEvents 1.0 envelope and the types a domain
// uses to raise its events.
//
// An [Event] carries the context attributes a service sets, and [Encode] and
// [Decode] map it to and from binary content mode: each attribute is a
// ce-prefixed header, datacontenttype is the content-type header, and the
// data travels as the message body. [Header] is a plain map, so it converts
// to a NATS or HTTP header without this package importing either.
//
// # Raising and emitting
//
// A domain raises, and the recorder emits. A domain declares each event it
// raises as an [EventKind], which pairs the event type with the Go type of
// its data, the event entity:
//
//	var Started = event.Define[StartedData]("exercise.started")
//
// A command raises its events into a [Queue] while it runs, and a
// [Recorder] emits them. [Recorder.Emit] wraps the command's body into the
// function a transaction runs. The body gets a fresh queue. Only when the
// body succeeds does the recorder stamp each queued event with an id, the
// source, and the time, and write it through the recorder's [Sink] into the
// same transaction. A body that fails writes nothing, so an event is emitted
// only in the transaction of the command that makes it true. Emit neither
// begins nor ends the transaction; the code that began it ends it.
//
// The transaction is a type parameter, so this package names no database.
// The composition root fixes the type when it builds the recorder over a
// sink, such as an outbox's, and passing a pool where the sink expects its
// transaction type fails to compile.
//
// A handler that must never see an event again wraps its error with
// [Permanent]. The package uses the standard library alone.
package event
