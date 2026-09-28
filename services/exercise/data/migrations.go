package data

import (
	"embed"
	"fmt"

	"github.com/standards-lab/sqlate/migrate"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Set names the service's own migration set, whose history the
// schema_version table keeps.
const Set = "exercise"

// Migrations is the service's migration sets in declaration order: the sets
// the libraries beneath the service ship, as the composition root passes
// them, then the service's own, NNNN_name.{up,down}.sql under migrations/,
// whose migrations may reference the tables beneath. A layout defect is a
// wiring defect and panics at cold start.
func Migrations(beneath ...migrate.Set) []migrate.Set {
	ms, err := migrate.Files(migrationFiles, "migrations")
	if err != nil {
		panic(fmt.Sprintf("data: %v", err))
	}
	return append(beneath, migrate.Set{Name: Set, Table: "schema_version", Migrations: ms})
}
