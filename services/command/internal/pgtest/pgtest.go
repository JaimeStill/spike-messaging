// Package pgtest gives each integration test of the service its own
// throwaway database on the server COMMAND_TEST_DSN names, so no test
// depends on the compose stack's database or on another test. [Open] and
// [OpenBare] return one to the domain tier, and [Scratch] names one for the
// integration harness to point a service process at. Each database is
// dropped when the test ends. It builds on messaging/postgres's pgtest.
package pgtest

import (
	"os"
	"testing"

	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/migrate"
	pgdialect "github.com/standards-lab/sqlate/postgres"
	"github.com/standards-lab/sqlate/query"

	"github.com/JaimeStill/spike-messaging/messaging/postgres"
	"github.com/JaimeStill/spike-messaging/messaging/postgres/pgtest"
	"github.com/JaimeStill/spike-messaging/services/command/data"
)

// DefaultDSN is the compose stack's administrative connection, used when
// COMMAND_TEST_DSN is unset.
const DefaultDSN = "postgres://app:app@127.0.0.1:5435/app?sslmode=disable"

// Open creates a uniquely named database, applies the messaging set and the
// service's own beneath it through the migrator, and returns the database
// as the domains see it, over the service's pattern catalog.
func Open(t testing.TB) *data.Database {
	t.Helper()
	db := OpenBare(t)
	messaging, err := postgres.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	m, err := migrate.New(db.DB, []migrate.Set{messaging}, migrate.Options{})
	if err != nil {
		t.Fatalf("migrator: %v", err)
	}
	if err := m.Up(t.Context()); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	return db
}

// OpenBare is Open without the migrations, for a test of the schema itself.
func OpenBare(t testing.TB) *data.Database {
	t.Helper()
	dsn := pgtest.DSN(t, adminDSN(), Scratch(t))
	pool := pgtest.Pool(t, dsn)
	return data.New(sqlate.Wrap(pool, pgdialect.Dialect{}), query.MustCatalog(query.Patterns()))
}

// Scratch creates a uniquely named, empty database, command_test_<hex>, on
// the administrative server and returns its name, dropping it when the test
// ends.
func Scratch(t testing.TB) string {
	t.Helper()
	return pgtest.Scratch(t, adminDSN(), "command")
}

// adminDSN is COMMAND_TEST_DSN, else the compose stack's [DefaultDSN].
func adminDSN() string {
	if dsn := os.Getenv("COMMAND_TEST_DSN"); dsn != "" {
		return dsn
	}
	return DefaultDSN
}
