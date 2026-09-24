//go:build integration

package outbox_test

import (
	"errors"
	"testing"

	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/migrate"

	"github.com/JaimeStill/spike-messaging/core/event"
	"github.com/JaimeStill/spike-messaging/internal/pgtest"
	"github.com/JaimeStill/spike-messaging/messaging/outbox"
)

// migrated returns a throwaway database with the messaging set applied.
func migrated(t *testing.T) *sqlate.DB {
	t.Helper()
	db := pgtest.Open(t)
	if err := migrator(t, db).Up(t.Context()); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	return db
}

func migrator(t *testing.T, db *sqlate.DB) *migrate.Migrator {
	t.Helper()
	set, err := outbox.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	m, err := migrate.New(db, []migrate.Set{set}, migrate.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func tick(id string) event.Event {
	return event.Event{ID: id, Source: "/outbox-test", Type: "lab.demo.tick", Data: []byte(`{"n":` + id + `}`)}
}

// emit writes events in one transaction and commits it.
func emit(t *testing.T, db *sqlate.DB, events ...event.Event) {
	t.Helper()
	if err := transact(t, db, events...); err != nil {
		t.Fatalf("emit: %v", err)
	}
}

func transact(t *testing.T, db *sqlate.DB, events ...event.Event) error {
	t.Helper()
	_, err := sqlate.Transact(t.Context(), db, func(tx *sqlate.Tx) (struct{}, error) {
		for _, e := range events {
			if err := outbox.NewEmitter().Emit(t.Context(), tx, e); err != nil {
				return struct{}{}, err
			}
		}
		return struct{}{}, nil
	})
	return err
}

// rows returns how many outbox rows exist, and how many are unpublished.
func rows(t *testing.T, db *sqlate.DB) (total, pending int) {
	t.Helper()
	r, err := db.QueryContext(t.Context(),
		`SELECT count(*), count(*) FILTER (WHERE published_at IS NULL) FROM messaging_outbox`)
	if err != nil {
		t.Fatalf("count rows: %v", err)
	}
	defer func() { _ = r.Close() }()
	r.Next()
	if err := r.Scan(&total, &pending); err != nil {
		t.Fatalf("scan counts: %v", err)
	}
	return total, pending
}

func exists(t *testing.T, db *sqlate.DB, table string) bool {
	t.Helper()
	r, err := db.QueryContext(t.Context(), `SELECT to_regclass($1) IS NOT NULL`, table)
	if err != nil {
		t.Fatalf("to_regclass %s: %v", table, err)
	}
	defer func() { _ = r.Close() }()
	var ok bool
	r.Next()
	if err := r.Scan(&ok); err != nil {
		t.Fatalf("scan %s: %v", table, err)
	}
	return ok
}

func TestMigrationsUpAndDown(t *testing.T) {
	db := pgtest.Open(t)
	m := migrator(t, db)
	if err := m.Up(t.Context()); err != nil {
		t.Fatalf("up: %v", err)
	}
	for _, table := range []string{"messaging_outbox", "messaging_inbox", outbox.Table} {
		if !exists(t, db, table) {
			t.Errorf("after up, %s is missing", table)
		}
	}
	if err := m.Down(t.Context(), 1); err != nil {
		t.Fatalf("down: %v", err)
	}
	for _, table := range []string{"messaging_outbox", "messaging_inbox"} {
		if exists(t, db, table) {
			t.Errorf("after down, %s remains", table)
		}
	}
}

func TestEmitIsVisibleOnlyAfterCommit(t *testing.T) {
	db := migrated(t)
	tx, err := db.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := outbox.NewEmitter().Emit(t.Context(), tx, tick("1")); err != nil {
		t.Fatalf("emit: %v", err)
	}
	if total, _ := rows(t, db); total != 0 {
		t.Fatalf("before commit, %d rows are visible", total)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if total, pending := rows(t, db); total != 1 || pending != 1 {
		t.Fatalf("after commit, rows = %d, pending = %d; want 1, 1", total, pending)
	}
}

func TestRollbackLeavesNoRow(t *testing.T) {
	db := migrated(t)
	boom := errors.New("mutation failed")
	_, err := sqlate.Transact(t.Context(), db, func(tx *sqlate.Tx) (struct{}, error) {
		if err := outbox.NewEmitter().Emit(t.Context(), tx, tick("1")); err != nil {
			return struct{}{}, err
		}
		return struct{}{}, boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("transact = %v, want %v", err, boom)
	}
	if total, _ := rows(t, db); total != 0 {
		t.Fatalf("after rollback, %d rows remain", total)
	}
}

func TestDuplicateEventFailsTheTransaction(t *testing.T) {
	db := migrated(t)
	emit(t, db, tick("1"))
	err := transact(t, db, tick("2"), tick("1"))
	if !errors.Is(err, sqlate.ErrUniqueViolation) {
		t.Fatalf("second emit of 1 = %v, want %v", err, sqlate.ErrUniqueViolation)
	}
	if total, _ := rows(t, db); total != 1 {
		t.Fatalf("rows = %d, want 1: the failed transaction's other event must not remain", total)
	}
}

func TestEmitRejectsAnInvalidEvent(t *testing.T) {
	db := migrated(t)
	if err := transact(t, db, event.Event{ID: "1"}); err == nil {
		t.Fatal("emit of an event without source or type succeeded")
	}
}
