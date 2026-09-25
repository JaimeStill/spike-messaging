//go:build integration

package app_test

import (
	"context"
	"database/sql"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	natsgo "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
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

// Each scenario that uses a broker runs end to end on the compose stack's
// NATS, and deletes its scratch stream when it ends. The request scenario
// runs its native request and reply there too, and creates no stream.
func TestScenariosOnNATS(t *testing.T) {
	if os.Getenv("MESSAGING_NATS_URL") == "" || os.Getenv("MESSAGING_DSN") == "" {
		t.Fatal("MESSAGING_NATS_URL or MESSAGING_DSN is not set; run under mise with the compose stack up")
	}
	before := courierStreams(t)
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"group", []string{"--events", "6", "--interval", "10ms", "--work", "20ms"}, "worker-b handled"},
		{"retry", []string{"--retry", "100ms"}, "two deliveries, the second after the retry delay"},
		{"permanent", []string{"--quiet", "300ms"}, "event 1 was delivered once and terminated"},
		{"drain", []string{"--work", "200ms"}, "its acknowledgement held"},
		{"outbox", []string{"--events", "3", "--poll", "20ms"}, "each of 3 events delivered once: 1 2 3"},
		{"request", []string{"--requests", "3"}, "each of 3 requests answered by its own reply"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			args := append([]string{"--broker", "nats", "scenario", c.name}, c.args...)
			code, out, errs := execute(t, args...)
			if code != process.ExitOK {
				t.Fatalf("exit %d\n%s%s", code, out, errs)
			}
			if !strings.Contains(out, c.want) {
				t.Errorf("narration lacks %q:\n%s", c.want, out)
			}
		})
	}
	for _, name := range courierStreams(t) {
		if !slices.Contains(before, name) {
			t.Errorf("stream %s is still on the server", name)
		}
	}
}

// courierStreams lists the streams on the server whose names courier gives
// its scratch streams.
func courierStreams(t *testing.T) []string {
	t.Helper()
	nc, err := natsgo.Connect(os.Getenv("MESSAGING_NATS_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	names := js.StreamNames(ctx)
	var found []string
	for name := range names.Name() {
		if strings.HasPrefix(name, "courier_") {
			found = append(found, name)
		}
	}
	if err := names.Err(); err != nil {
		t.Fatal(err)
	}
	return found
}
