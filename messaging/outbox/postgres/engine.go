package postgres

import (
	"context"
	"embed"

	"github.com/standards-lab/sqlate"
	pgdialect "github.com/standards-lab/sqlate/postgres"
	"github.com/standards-lab/sqlate/query"

	"github.com/JaimeStill/spike-messaging/messaging/outbox"
)

//go:embed statements/*.sql
var statementFiles embed.FS

// statements is the outbox's SQL, compiled once for Postgres. The files
// include no patterns, so the catalog is sqlate's own. A compile error is a
// defect in the embedded files, which any test of the package finds.
var statements = query.MustCatalog(query.Patterns()).MustCompile(statementFiles, "statements", pgdialect.Dialect{})

// Engine returns the outbox's statements for Postgres, for [outbox.New].
func Engine() outbox.Engine {
	return outbox.Engine{
		Emit:          statements.Statement("emit"),
		ClaimRow:      statements.Statement("claim_row"),
		MarkPublished: statements.Statement("mark_published"),
		ClaimInbox:    statements.Statement("claim_inbox"),
	}
}

// countPending is outside the Engine contract: the outbox itself never
// counts its rows.
var countPending = statements.Statement("count_pending").Scan(query.Scalar[int])

// Pending counts the outbox rows the relay has not yet published, for a
// consumer that reports or monitors the outbox's lag.
func Pending(ctx context.Context, sess sqlate.Session) (int, error) {
	return countPending.One(ctx, sess, nil)
}

// Statements returns the compiled statements in name order, for a consumer
// that lists the SQL its program runs.
func Statements() []query.Statement { return statements.Statements() }

// Verify prepares the statements against the schema sess reaches, so a
// schema without the messaging set, or a set behind these statements, fails
// at start rather than at the first emit. A consumer calls it from its
// verify stage, after the migrations.
func Verify(ctx context.Context, sess sqlate.Session) error { return statements.Verify(ctx, sess) }
