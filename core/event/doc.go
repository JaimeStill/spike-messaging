// Package event is the CloudEvents 1.0 envelope and what a domain depends on
// to emit one.
//
// An [Event] carries the context attributes a service sets, and [Encode] and
// [Decode] map it to and from binary content mode: each attribute is a
// ce-prefixed header, datacontenttype is the content-type header, and the
// data travels as the message body. [Header] is a plain map, so it converts
// to a NATS or HTTP header without this package importing either.
//
// A domain emits through an [Emitter], writing the event in the same [Tx] as
// the mutation it reports. A handler that must never see an event again
// wraps its error with [Permanent]. The package uses the standard library
// alone.
package event
