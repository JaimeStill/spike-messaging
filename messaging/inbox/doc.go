// Package inbox is a consumer's guard against handling a redelivered event
// twice, on any SQL engine that supplies its statement.
//
// Delivery is at least once, so a handler can see an event again. An
// [Inbox] is built with [New] over an [Engine], the statement one engine's
// module compiles, such as messaging/postgres, which ships the table in the
// same migration set as the outbox's. [Inbox.Claim] records, in the
// handler's own transaction, that a consumer is handling an event, and
// reports whether it is the first time. The claim commits with the
// handler's work or rolls back with it, so a handler that fails leaves the
// event to be claimed again on its redelivery. This package holds no SQL and
// imports no driver.
package inbox
