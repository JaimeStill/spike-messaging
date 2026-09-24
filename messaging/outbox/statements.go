package outbox

import (
	"context"
	"embed"

	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/postgres"
	"github.com/standards-lab/sqlate/query"
)

//go:embed statements/*.sql
var statementFiles embed.FS

// statements is the package's statements, compiled once for Postgres. The files
// include no patterns, so the catalog is sqlate's own. A compile error is a
// defect in the embedded files, which any test of the package finds.
var statements = query.MustCatalog(query.Patterns()).MustCompile(statementFiles, "statements", postgres.Dialect{})

var (
	emitStmt       = statements.Statement("emit")
	claimRowStmt   = statements.Statement("claim_row").Scan(scanRow)
	markStmt       = statements.Statement("mark_published")
	claimInboxStmt = statements.Statement("claim_inbox")
)

// row is one claimed outbox row.
type row struct {
	seq    int64
	header []byte
	data   []byte
}

func scanRow(r query.Row) (row, error) {
	var v row
	err := r.Scan(&v.seq, &v.header, &v.data)
	return v, err
}

// Statements returns the package's compiled statements in name order, for a
// consumer that lists the SQL its program runs.
func Statements() []query.Statement { return statements.Statements() }

// Verify prepares the package's statements against the schema sess
// reaches, so a schema without the messaging set, or a set behind these
// statements, fails at start rather than at the first emit. A consumer
// calls it from its verify stage, after the migrations.
func Verify(ctx context.Context, sess sqlate.Session) error { return statements.Verify(ctx, sess) }
