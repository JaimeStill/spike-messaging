//go:build integration

package app_test

import (
	"database/sql"
	"os"
	"regexp"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/standards-lab/go-core/process"
)

// The outbox scenario runs end to end on the compose stack's Postgres: every
// event committed before the relay starts is delivered once, no row is left
// pending, and the scratch database is dropped.
func TestOutboxScenarioOnPostgres(t *testing.T) {
	dsn := os.Getenv("MESSAGING_DSN")
	if dsn == "" {
		t.Fatal("MESSAGING_DSN is not set; run under mise with the compose stack up")
	}
	code, out, errs := execute(t, "scenario", "outbox", "--events", "4", "--poll", "20ms")
	if code != process.ExitOK {
		t.Fatalf("exit %d\n%s%s", code, out, errs)
	}
	for _, want := range []string{
		"4 rows pending",
		"delivered event 1", "delivered event 2", "delivered event 3", "delivered event 4",
		"drained cleanly",
		"each of 4 events delivered once: 1 2 3 4",
		"0 rows pending",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("narration lacks %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "4 rows pending") > strings.Index(out, "delivered event 1") {
		t.Errorf("the rows were not pending before the relay ran:\n%s", out)
	}

	m := regexp.MustCompile(`database (courier_outbox_[0-9a-f]{8}) ready`).FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("narration names no scratch database:\n%s", out)
	}
	if !strings.Contains(out, "dropped database "+m[1]) {
		t.Errorf("narration lacks the drop of %s:\n%s", m[1], out)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var n int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM pg_database WHERE datname = $1`, m[1]).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("database %s is still on the server", m[1])
	}
}
