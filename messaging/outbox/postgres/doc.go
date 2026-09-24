// Package postgres is the outbox's Postgres engine: the statements
// [outbox.New] runs, from [Engine], and the tables they run against, from
// [Migrations].
//
// The migration set is named [Source] and records its history in [Table].
// Every object it creates is prefixed messaging_, and a consumer declares it
// ahead of its own set. The statements are authored files compiled through
// sqlate's query package; [Verify] prepares them against the live schema,
// for a consumer's verify stage.
package postgres
