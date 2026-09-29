//go:build integration

package postgres_test

import (
	"errors"
	"testing"

	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/migrate"

	"github.com/JaimeStill/spike-messaging/core/event"
	"github.com/JaimeStill/spike-messaging/messaging/inbox"
	"github.com/JaimeStill/spike-messaging/messaging/outbox"
	"github.com/JaimeStill/spike-messaging/messaging/postgres"
	"github.com/JaimeStill/spike-messaging/messaging/postgres/internal/pgtest"
)

// ob and in are the outbox and the inbox on the Postgres engine, which every
// test runs.
var (
	ob = must(outbox.New(postgres.Outbox()))
	in = must(inbox.New(postgres.Inbox()))
)

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

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
	set, err := postgres.Migrations()
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

// emit writes events through the sink in one transaction and commits it.
func emit(t *testing.T, db *sqlate.DB, events ...event.Event) {
	t.Helper()
	if err := transact(t, db, events...); err != nil {
		t.Fatalf("emit: %v", err)
	}
}

func transact(t *testing.T, db *sqlate.DB, events ...event.Event) error {
	t.Helper()
	_, err := sqlate.Transact(t.Context(), db, func(tx *sqlate.Tx) (struct{}, error) {
		return struct{}{}, ob.Sink().Write(t.Context(), tx, events...)
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
	for _, table := range []string{"messaging_outbox", "messaging_inbox", postgres.Table} {
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

func TestVerifyNeedsTheMigratedSchema(t *testing.T) {
	bare := pgtest.Open(t)
	if err := postgres.Verify(t.Context(), bare); err == nil {
		t.Fatal("Verify passed on a database without the messaging set")
	}
	if err := postgres.Verify(t.Context(), migrated(t)); err != nil {
		t.Fatalf("Verify on the migrated schema: %v", err)
	}
}

func TestEmitIsVisibleOnlyAfterCommit(t *testing.T) {
	db := migrated(t)
	tx, err := db.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := ob.Sink().Write(t.Context(), tx, tick("1")); err != nil {
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
		if err := ob.Sink().Write(t.Context(), tx, tick("1")); err != nil {
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

func TestPendingCountsUnpublishedRows(t *testing.T) {
	db := migrated(t)
	emit(t, db, tick("1"), tick("2"))
	if _, err := db.ExecContext(t.Context(), `UPDATE messaging_outbox SET published_at = now() WHERE id = '1'`); err != nil {
		t.Fatal(err)
	}
	n, err := postgres.Pending(t.Context(), db)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("Pending = %d, want 1", n)
	}
}

type ticked struct {
	N int `json:"n"`
}

var kindTicked = event.Define[ticked]("lab.demo.ticked")

// A recorder over the outbox's sink writes a command's raised events into
// the command's transaction when its body succeeds, and nothing when it
// fails, so the rows exist exactly when the state they report committed.
func TestRecorderEmitsIntoTheCommandsTransaction(t *testing.T) {
	db := migrated(t)
	rec := event.NewRecorder(ob.Sink(), "/outbox-test")
	body := func(fail error) func(*sqlate.Tx, *event.Queue) (int, error) {
		return func(tx *sqlate.Tx, q *event.Queue) (int, error) {
			if _, err := tx.ExecContext(t.Context(), `SELECT 1`); err != nil {
				return 0, err
			}
			kindTicked.Raise(q, "ex-1", ticked{N: 1})
			kindTicked.Raise(q, "ex-1", ticked{N: 2})
			return 2, fail
		}
	}
	boom := errors.New("command refused")
	if _, err := sqlate.Transact(t.Context(), db, rec.Emit(t.Context(), body(boom))); !errors.Is(err, boom) {
		t.Fatalf("failing command = %v, want %v", err, boom)
	}
	if total, _ := rows(t, db); total != 0 {
		t.Fatalf("after a failed command, %d rows exist", total)
	}
	n, err := sqlate.Transact(t.Context(), db, rec.Emit(t.Context(), body(nil)))
	if err != nil || n != 2 {
		t.Fatalf("command = %d, %v", n, err)
	}
	r, err := db.QueryContext(t.Context(), `SELECT source, data FROM messaging_outbox ORDER BY seq`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	var got []string
	for r.Next() {
		var source, data string
		if err := r.Scan(&source, &data); err != nil {
			t.Fatal(err)
		}
		got = append(got, source+" "+data)
	}
	want := []string{`/outbox-test {"n":1}`, `/outbox-test {"n":2}`}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("rows = %q, want %q in raise order", got, want)
	}
}
