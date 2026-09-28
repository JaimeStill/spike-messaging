//go:build integration

// Package pgtest gives each integration-tagged test of the service its own
// throwaway database on the server EXERCISE_TEST_DSN names, with the
// service's migration sets applied, so no test depends on the compose
// stack's database or on another test. The database is dropped when the
// test ends. It follows messaging/postgres's pgtest.
package pgtest

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net/url"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/migrate"
	pgdialect "github.com/standards-lab/sqlate/postgres"
	"github.com/standards-lab/sqlate/query"

	"github.com/JaimeStill/spike-messaging/messaging/postgres"
	"github.com/JaimeStill/spike-messaging/services/exercise/data"
)

// DefaultDSN is the compose stack's administrative connection, used when
// EXERCISE_TEST_DSN is unset.
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
	m, err := migrate.New(db.DB, data.Migrations(messaging), migrate.Options{})
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
	dsn := os.Getenv("EXERCISE_TEST_DSN")
	if dsn == "" {
		dsn = DefaultDSN
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open %s: %v", dsn, err)
	}
	name := "exercise_test_" + suffix(t)
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+name); err != nil {
		// A create that timed out can still finish on the server, so the
		// database is dropped if it exists, on a context of its own.
		dctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_, _ = admin.ExecContext(dctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
		cancel()
		_ = admin.Close()
		t.Fatalf("create database %s: %v", name, err)
	}
	t.Cleanup(func() {
		// WITH (FORCE) ends the database's other sessions, and the server can
		// still report one closing, so the drop retries with a deadline each.
		var err error
		for range 4 {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			_, err = admin.ExecContext(ctx, "DROP DATABASE "+name+" WITH (FORCE)")
			cancel()
			if err == nil {
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
		if err != nil {
			t.Errorf("drop database %s: %v", name, err)
		}
		_ = admin.Close()
	})

	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse %s: %v", dsn, err)
	}
	u.Path = "/" + name
	pool, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	if err := pool.PingContext(ctx); err != nil {
		t.Fatalf("ping %s: %v", name, err)
	}
	return data.New(sqlate.Wrap(pool, pgdialect.Dialect{}), query.MustCatalog(query.Patterns()))
}

// suffix returns eight random hex characters, so parallel packages never
// pick the same database name.
func suffix(t testing.TB) string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("random suffix: %v", err)
	}
	return hex.EncodeToString(b[:])
}
