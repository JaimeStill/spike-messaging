// Package data is the service's database as its domains see it: the sqlate
// session over the pool, with the dialect, grouped with the pattern catalog
// every statement compiles against.
package data

import (
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/query"
)

// Database is the database as a domain sees it: the session, and the
// catalog of every pattern namespace the composition root registered. A
// domain compiles its statements against Catalog and runs them through the
// session, beginning its commands' transactions on it.
type Database struct {
	*sqlate.DB
	Catalog *query.Catalog
}

// New groups a session with the catalog its statements compile against. No
// I/O happens here.
func New(db *sqlate.DB, catalog *query.Catalog) *Database {
	return &Database{DB: db, Catalog: catalog}
}
