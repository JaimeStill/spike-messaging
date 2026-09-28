// Package postgres is the messaging layer's Postgres engine: the statements
// [outbox.New] runs, from [Outbox], the statement [inbox.New] runs, from
// [Inbox], and the tables they run against, from [Migrations].
//
// The migration set is named [Source] and records its history in [Table].
// It holds both the outbox's table and the inbox's, so one module ships them
// in one released migration. Every object it creates is prefixed
// messaging_, and a consumer declares the set ahead of its own. The
// statements are authored files compiled through sqlate's query package;
// [Verify] prepares them against the live schema, for a consumer's verify
// stage.
package postgres
