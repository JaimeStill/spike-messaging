//go:build integration

// Package pgtest gives each integration-tagged test its own throwaway
// database on the server MESSAGING_DSN names, so no test depends on the
// compose stack's database or on another test. The database is dropped when
// the test ends. It follows spike-blobfs's livetest.
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
	"github.com/standards-lab/sqlate/postgres"
)

// Open creates a uniquely named database and returns a session over a pool
// connected to it. A missing MESSAGING_DSN fails the test: the integration
// tag states that the stack is expected.
func Open(t testing.TB) *sqlate.DB {
	t.Helper()
	db, _ := OpenDSN(t)
	return db
}

// OpenDSN is Open, also returning the throwaway database's DSN, for a test
// that hands the DSN to code that opens its own pool.
func OpenDSN(t testing.TB) (*sqlate.DB, string) {
	t.Helper()
	dsn := os.Getenv("MESSAGING_DSN")
	if dsn == "" {
		t.Fatal("MESSAGING_DSN is not set; run under mise with the compose stack up")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open %s: %v", dsn, err)
	}
	name := "messaging_test_" + suffix(t)
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+name); err != nil {
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
	testDSN := u.String()
	pool, err := sql.Open("pgx", testDSN)
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	if err := pool.PingContext(ctx); err != nil {
		t.Fatalf("ping %s: %v", name, err)
	}
	return sqlate.Wrap(pool, postgres.Dialect{}), testDSN
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
