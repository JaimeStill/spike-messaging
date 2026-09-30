//go:build integration

package exercise_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/migrate"

	"github.com/JaimeStill/spike-messaging/core/event"
	"github.com/JaimeStill/spike-messaging/messaging/inbox"
	"github.com/JaimeStill/spike-messaging/messaging/outbox"
	"github.com/JaimeStill/spike-messaging/messaging/postgres"
	"github.com/JaimeStill/spike-messaging/services/exercise/data"
	"github.com/JaimeStill/spike-messaging/services/exercise/domain/exercise"
	"github.com/JaimeStill/spike-messaging/services/exercise/domain/exercise/rules"
	"github.com/JaimeStill/spike-messaging/services/exercise/internal/pgtest"
)

// ob and in are the outbox and the inbox on the Postgres engine, as the
// composition root builds them.
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

// setup returns the service over a throwaway, migrated database, with the
// real recorder over the outbox's sink, and the database for the test's
// own reads.
func setup(t *testing.T) (*exercise.Service, *data.Database) {
	t.Helper()
	db := pgtest.Open(t)
	svc := exercise.New(db, event.NewRecorder(ob.Sink(), "/exercise"))
	if err := svc.Verify(t.Context()); err != nil {
		t.Fatalf("verify: %v", err)
	}
	return svc, db
}

// emitted is one outbox row, decoded.
type emitted struct {
	Type, Subject string
	Data          json.RawMessage
}

// outboxRows returns the events the outbox holds, in the order they were
// written.
func outboxRows(t *testing.T, db *data.Database) []emitted {
	t.Helper()
	r, err := db.QueryContext(t.Context(), `SELECT header, data FROM messaging_outbox ORDER BY seq`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	var out []emitted
	for r.Next() {
		var header, body []byte
		if err := r.Scan(&header, &body); err != nil {
			t.Fatal(err)
		}
		var h event.Header
		if err := json.Unmarshal(header, &h); err != nil {
			t.Fatal(err)
		}
		e, err := event.Decode(h, body)
		if err != nil {
			t.Fatalf("decode outbox row: %v", err)
		}
		if e.Source != "/exercise" {
			t.Errorf("event %s source = %q", e.Type, e.Source)
		}
		out = append(out, emitted{Type: e.Type, Subject: e.Subject, Data: e.Data})
	}
	if err := r.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// scalar runs a query of one row and one column and returns its value.
func scalar[T any](t *testing.T, db *data.Database, q string, args ...any) T {
	t.Helper()
	r, err := db.QueryContext(t.Context(), q, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	var v T
	if !r.Next() {
		t.Fatalf("%s: no row", q)
	}
	if err := r.Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func types(es []emitted) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Type
	}
	return out
}

// started creates and starts the fixture as changed by change.
func started(t *testing.T, svc *exercise.Service, change func(*exercise.CreateExercise)) exercise.Exercise {
	t.Helper()
	c := fixture()
	if change != nil {
		change(&c)
	}
	ex, err := svc.Create(t.Context(), c)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	ex, err = svc.Start(t.Context(), ex.ID)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	return ex
}

// resolveOnce waits past the exercise's due time and resolves until one
// round resolves.
func resolveOnce(t *testing.T, svc *exercise.Service) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(15 * time.Millisecond)
		n, err := svc.ResolveDue(t.Context())
		if err != nil {
			t.Fatalf("resolve due: %v", err)
		}
		if n > 0 {
			return
		}
	}
	t.Fatal("no round resolved within 5s")
}

// element returns the element of ex's state with the id.
func element(t *testing.T, ex exercise.Exercise, id string) rules.Element {
	t.Helper()
	for _, e := range ex.State.Elements {
		if e.ID == id {
			return e
		}
	}
	t.Fatalf("exercise has no element %s", id)
	return rules.Element{}
}

func find(t *testing.T, svc *exercise.Service, id string) exercise.Exercise {
	t.Helper()
	ex, err := svc.Find(t.Context(), id)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	return ex
}

func TestMigrationsUpAndDown(t *testing.T) {
	db := pgtest.OpenBare(t)
	messaging, err := postgres.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	m, err := migrate.New(db.DB, data.Migrations(messaging), migrate.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Up(t.Context()); err != nil {
		t.Fatalf("up: %v", err)
	}
	if err := m.Down(t.Context(), 1); err != nil {
		t.Fatalf("down 0002: %v", err)
	}
	for _, col := range [][2]string{{"exercise", "seed"}, {"exercise_round", "resolution"}} {
		if scalar[bool](t, db, `SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = $1 AND column_name = $2)`, col[0], col[1]) {
			t.Errorf("after 0002's down, %s.%s remains", col[0], col[1])
		}
	}
	if err := m.Down(t.Context(), 1); err != nil {
		t.Fatalf("down 0001: %v", err)
	}
	for _, table := range []string{"exercise", "exercise_round", "exercise_orders"} {
		if scalar[bool](t, db, `SELECT to_regclass($1) IS NOT NULL`, table) {
			t.Errorf("after down, %s remains", table)
		}
	}
}

func TestVerifyNeedsTheMigratedSchema(t *testing.T) {
	bare := pgtest.OpenBare(t)
	svc := exercise.New(bare, event.NewRecorder(ob.Sink(), "/exercise"))
	if err := svc.Verify(t.Context()); err == nil {
		t.Fatal("Verify passed on a database without the exercise set")
	}
}

// With no orders, two factions apart stand still until the round limit,
// and the exercise concludes in a draw. Each round's observations and the
// conclusion reach the outbox in the order they were raised, and the
// history records each resolved round with the resolution its event
// reported.
func TestIdleExerciseRunsToADraw(t *testing.T) {
	svc, db := setup(t)
	ex := started(t, svc, nil)
	if ex.Status != exercise.StatusRunning || ex.Round != 0 || ex.NextRoundAt == nil {
		t.Fatalf("started = %+v", ex)
	}
	for ex.Status == exercise.StatusRunning {
		resolveOnce(t, svc)
		ex = find(t, svc, ex.ID)
	}
	if ex.Status != exercise.StatusConcluded || ex.Round != 3 || ex.NextRoundAt != nil {
		t.Fatalf("concluded = %+v", ex)
	}
	if want := (rules.Verdict{Over: true, Reason: "limit"}); ex.Verdict == nil || *ex.Verdict != want {
		t.Fatalf("verdict = %+v, want %+v", ex.Verdict, want)
	}

	history, err := svc.History(t.Context(), ex.ID)
	if err != nil {
		t.Fatal(err)
	}
	var rounds []int
	for _, r := range history {
		rounds = append(rounds, r.Round)
	}
	if !slices.Equal(rounds, []int{0, 1, 2, 3}) {
		t.Fatalf("history rounds = %v", rounds)
	}
	if !history[3].Verdict.Over || history[2].Verdict.Over || history[0].Observations[1].Faction != "blue" {
		t.Errorf("history = %+v", history)
	}
	if history[0].Resolution != nil {
		t.Errorf("round 0 has a resolution: %+v", history[0].Resolution)
	}

	es := outboxRows(t, db)
	want := []string{"exercise.started", "exercise.round.observed", "exercise.round.observed"}
	for range 3 {
		want = append(want, "exercise.round.resolved", "exercise.round.observed", "exercise.round.observed")
	}
	want = append(want, "exercise.concluded")
	if got := types(es); !slices.Equal(got, want) {
		t.Fatalf("outbox = %v, want %v", got, want)
	}
	for i, e := range es {
		if e.Subject != ex.ID {
			t.Errorf("event %d subject = %q, want %s", i, e.Subject, ex.ID)
		}
	}
	var observed, resolved int
	for _, e := range es {
		switch e.Type {
		case "exercise.round.observed":
			var d exercise.ObservedData
			if err := json.Unmarshal(e.Data, &d); err != nil {
				t.Fatal(err)
			}
			if wantRound, wantFaction := observed/2, ex.Factions[observed%2]; d.Round != wantRound || d.Faction != wantFaction {
				t.Errorf("observed %d = round %d of %s, want round %d of %s", observed, d.Round, d.Faction, wantRound, wantFaction)
			}
			observed++
		case "exercise.round.resolved":
			resolved++
			var d exercise.ResolvedData
			if err := json.Unmarshal(e.Data, &d); err != nil {
				t.Fatal(err)
			}
			if d.Exercise != ex.ID || d.Round != resolved || d.Retreats == nil || d.Engagements == nil ||
				d.Losses == nil || d.Captures == nil || d.Progress == nil {
				t.Errorf("resolved %d = %+v, want round %d with empty lists", resolved, d, resolved)
			}
			if got := history[resolved].Resolution; got == nil || !reflect.DeepEqual(*got, d.Resolution) {
				t.Errorf("history of round %d has resolution %+v, want the event's %+v", resolved, got, d.Resolution)
			}
		}
	}
	var c exercise.ConcludedData
	if err := json.Unmarshal(es[len(es)-1].Data, &c); err != nil {
		t.Fatal(err)
	}
	if c != (exercise.ConcludedData{Exercise: ex.ID, Round: 3, Winner: "", Reason: "limit"}) {
		t.Errorf("concluded = %+v", c)
	}
	var s exercise.StartedData
	if err := json.Unmarshal(es[0].Data, &s); err != nil {
		t.Fatal(err)
	}
	if s.RoundIntervalMS != 10 || s.RoundLimit != 3 || s.Factions != ex.Factions || s.Name != "fixture" {
		t.Errorf("started = %+v", s)
	}
}

// A command that fails emits nothing: the events commit only with the
// state they report.
func TestAFailedCommandEmitsNothing(t *testing.T) {
	svc, db := setup(t)
	ex := started(t, svc, nil)
	before := len(outboxRows(t, db))
	if _, err := svc.Start(t.Context(), ex.ID); !errors.Is(err, exercise.ErrConflict) {
		t.Fatalf("second start = %v, want ErrConflict", err)
	}
	if after := len(outboxRows(t, db)); after != before {
		t.Fatalf("the failed start left %d outbox rows, want %d", after, before)
	}
}

// A start whose own verdict is already over concludes at once.
func TestStartConcludesAnExerciseAlreadyOver(t *testing.T) {
	svc, db := setup(t)
	ex := started(t, svc, func(c *exercise.CreateExercise) { c.Elements = c.Elements[:1] })
	if ex.Status != exercise.StatusConcluded || ex.Verdict == nil || ex.Verdict.Winner != "red" || ex.NextRoundAt != nil {
		t.Fatalf("started = %+v", ex)
	}
	want := []string{"exercise.started", "exercise.round.observed", "exercise.round.observed", "exercise.concluded"}
	if got := types(outboxRows(t, db)); !slices.Equal(got, want) {
		t.Fatalf("outbox = %v, want %v", got, want)
	}
}

// An order for a round already resolved is skipped and stored nowhere; an
// order for the next round is applied when it resolves.
func TestRecordOrdersSkipsAPastRound(t *testing.T) {
	svc, db := setup(t)
	ex := started(t, svc, func(c *exercise.CreateExercise) { c.RoundLimit = 10 })
	move := []rules.Order{{Element: "r1", Steps: []rules.Location{loc(1, 0)}}}

	if err := svc.RecordOrders(t.Context(), exercise.RecordOrders{Exercise: ex.ID, Faction: "red", Round: 0, Orders: move}, nil); err != nil {
		t.Fatalf("orders for round 0 = %v, want a skip", err)
	}
	if n := scalar[int](t, db, `SELECT count(*) FROM exercise_orders`); n != 0 {
		t.Fatalf("the skipped orders left %d rows", n)
	}

	if err := svc.RecordOrders(t.Context(), exercise.RecordOrders{Exercise: ex.ID, Faction: "red", Round: 1, Orders: move}, nil); err != nil {
		t.Fatalf("orders for round 1: %v", err)
	}
	resolveOnce(t, svc)
	ex = find(t, svc, ex.ID)
	if ex.Round != 1 || element(t, ex, "r1").At != loc(1, 0) {
		t.Fatalf("after round 1, r1 is at %+v in round %d", element(t, ex, "r1").At, ex.Round)
	}

	if err := svc.RecordOrders(t.Context(), exercise.RecordOrders{Exercise: ex.ID, Faction: "red", Round: 1, Orders: move}, nil); err != nil {
		t.Fatalf("orders for the resolved round 1 = %v, want a skip", err)
	}
}

// An order for an exercise that stopped is skipped, and its claim holds, so
// a redelivery is a repeat.
func TestRecordOrdersSkipsAnEndedExercise(t *testing.T) {
	svc, db := setup(t)
	ex := started(t, svc, nil)
	if _, err := svc.Stop(t.Context(), ex.ID); err != nil {
		t.Fatal(err)
	}
	issued := event.Event{ID: "orders-late", Source: "/operations", Type: "operations.orders.issued"}
	claim := func(ctx context.Context, tx *sqlate.Tx) (bool, error) {
		return in.Claim(ctx, tx, "exercise-orders", issued)
	}
	move := []rules.Order{{Element: "r1", Steps: []rules.Location{loc(1, 0)}}}
	if err := svc.RecordOrders(t.Context(), exercise.RecordOrders{Exercise: ex.ID, Faction: "red", Round: 1, Orders: move}, claim); err != nil {
		t.Fatalf("orders for a stopped exercise = %v, want a skip", err)
	}
	if n := scalar[int](t, db, `SELECT count(*) FROM exercise_orders`); n != 0 {
		t.Fatalf("the skipped orders left %d rows", n)
	}
	if n := scalar[int](t, db, `SELECT count(*) FROM messaging_inbox WHERE id = 'orders-late'`); n != 1 {
		t.Fatalf("the skip left %d claims, want 1", n)
	}
}

// The refusals no redelivery could fix are all permanent.
func TestRecordOrdersRefusals(t *testing.T) {
	svc, _ := setup(t)
	ex := started(t, svc, nil)
	cases := []struct {
		name string
		cmd  exercise.RecordOrders
		want error
	}{
		{"unknown exercise", exercise.RecordOrders{Exercise: "019300f0-0000-7000-8000-000000000000", Faction: "red", Round: 1}, exercise.ErrNotFound},
		{"not a uuid", exercise.RecordOrders{Exercise: "nope", Faction: "red", Round: 1}, exercise.ErrNotFound},
		{"unknown faction", exercise.RecordOrders{Exercise: ex.ID, Faction: "green", Round: 1}, exercise.ErrValidation},
		{"past the round limit", exercise.RecordOrders{Exercise: ex.ID, Faction: "red", Round: ex.RoundLimit + 1}, exercise.ErrValidation},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := svc.RecordOrders(t.Context(), tc.cmd, nil)
			if !event.IsPermanent(err) || !errors.Is(err, tc.want) {
				t.Fatalf("RecordOrders = %v, want permanent %v", err, tc.want)
			}
		})
	}
}

// A claim over the inbox makes a redelivered event change nothing: the
// second delivery's different orders are never recorded.
func TestRecordOrdersClaimsTheEvent(t *testing.T) {
	svc, _ := setup(t)
	ex := started(t, svc, nil)
	issued := event.Event{ID: "orders-1", Source: "/operations", Type: "operations.orders.issued"}
	claim := func(ctx context.Context, tx *sqlate.Tx) (bool, error) {
		return in.Claim(ctx, tx, "exercise-orders", issued)
	}
	first := exercise.RecordOrders{Exercise: ex.ID, Faction: "red", Round: 1,
		Orders: []rules.Order{{Element: "r1", Steps: []rules.Location{loc(1, 0)}}}}
	second := first
	second.Orders = []rules.Order{{Element: "r1", Steps: []rules.Location{loc(0, 1)}}}
	if err := svc.RecordOrders(t.Context(), first, claim); err != nil {
		t.Fatalf("first delivery: %v", err)
	}
	if err := svc.RecordOrders(t.Context(), second, claim); err != nil {
		t.Fatalf("redelivery: %v", err)
	}
	resolveOnce(t, svc)
	if at := element(t, find(t, svc, ex.ID), "r1").At; at != loc(1, 0) {
		t.Fatalf("r1 is at %+v, want the first delivery's 1,0", at)
	}
}

// Without a claim, a faction's later orders for a round replace its
// earlier ones.
func TestRecordOrdersLastWins(t *testing.T) {
	svc, _ := setup(t)
	ex := started(t, svc, nil)
	for _, to := range []rules.Location{loc(1, 0), loc(0, 1)} {
		cmd := exercise.RecordOrders{Exercise: ex.ID, Faction: "red", Round: 1,
			Orders: []rules.Order{{Element: "r1", Steps: []rules.Location{to}}}}
		if err := svc.RecordOrders(t.Context(), cmd, nil); err != nil {
			t.Fatal(err)
		}
	}
	resolveOnce(t, svc)
	if at := element(t, find(t, svc, ex.ID), "r1").At; at != loc(0, 1) {
		t.Fatalf("r1 is at %+v, want the later orders' 0,1", at)
	}
}

// A faction's order for the other faction's element is ignored.
func TestOrdersForTheOtherFactionAreIgnored(t *testing.T) {
	svc, _ := setup(t)
	ex := started(t, svc, nil)
	cmd := exercise.RecordOrders{Exercise: ex.ID, Faction: "blue", Round: 1,
		Orders: []rules.Order{
			{Element: "r1", Steps: []rules.Location{loc(1, 0)}},
			{Element: "b1", Steps: []rules.Location{loc(4, 5)}},
		}}
	if err := svc.RecordOrders(t.Context(), cmd, nil); err != nil {
		t.Fatal(err)
	}
	resolveOnce(t, svc)
	ex = find(t, svc, ex.ID)
	if at := element(t, ex, "r1").At; at != loc(0, 0) {
		t.Errorf("r1 is at %+v, want 0,0: blue cannot command it", at)
	}
	if at := element(t, ex, "b1").At; at != loc(4, 5) {
		t.Errorf("b1 is at %+v, want its own order's 4,5", at)
	}
}

// Pause holds the rounds, Resume continues them, and Stop of a running
// exercise concludes it as stopped, while Stop of one never started
// raises nothing.
func TestPauseResumeStop(t *testing.T) {
	svc, db := setup(t)
	ex := started(t, svc, func(c *exercise.CreateExercise) { c.RoundLimit = 10 })

	ex, err := svc.Pause(t.Context(), ex.ID)
	if err != nil || ex.Status != exercise.StatusPaused || ex.NextRoundAt != nil {
		t.Fatalf("pause = %+v, %v", ex, err)
	}
	if _, err := svc.Pause(t.Context(), ex.ID); !errors.Is(err, exercise.ErrConflict) {
		t.Fatalf("second pause = %v, want ErrConflict", err)
	}
	time.Sleep(30 * time.Millisecond)
	if n, err := svc.ResolveDue(t.Context()); err != nil || n != 0 {
		t.Fatalf("resolve while paused = %d, %v; want 0", n, err)
	}

	ex, err = svc.Resume(t.Context(), ex.ID)
	if err != nil || ex.Status != exercise.StatusRunning || ex.NextRoundAt == nil {
		t.Fatalf("resume = %+v, %v", ex, err)
	}
	if _, err := svc.Resume(t.Context(), ex.ID); !errors.Is(err, exercise.ErrConflict) {
		t.Fatalf("second resume = %v, want ErrConflict", err)
	}
	resolveOnce(t, svc)

	before := len(outboxRows(t, db))
	ex, err = svc.Stop(t.Context(), ex.ID)
	if err != nil || ex.Status != exercise.StatusStopped || ex.NextRoundAt != nil {
		t.Fatalf("stop = %+v, %v", ex, err)
	}
	if want := (rules.Verdict{Over: true, Reason: "stopped"}); ex.Verdict == nil || *ex.Verdict != want {
		t.Fatalf("verdict = %+v", ex.Verdict)
	}
	es := outboxRows(t, db)
	if len(es) != before+1 || es[len(es)-1].Type != "exercise.concluded" {
		t.Fatalf("outbox after stop = %v", types(es))
	}
	var c exercise.ConcludedData
	if err := json.Unmarshal(es[len(es)-1].Data, &c); err != nil {
		t.Fatal(err)
	}
	if c != (exercise.ConcludedData{Exercise: ex.ID, Round: 1, Reason: "stopped"}) {
		t.Errorf("concluded = %+v", c)
	}
	if _, err := svc.Stop(t.Context(), ex.ID); !errors.Is(err, exercise.ErrConflict) {
		t.Fatalf("second stop = %v, want ErrConflict", err)
	}
	time.Sleep(30 * time.Millisecond)
	if n, err := svc.ResolveDue(t.Context()); err != nil || n != 0 {
		t.Fatalf("resolve after stop = %d, %v; want 0", n, err)
	}

	created, err := svc.Create(t.Context(), fixture())
	if err != nil {
		t.Fatal(err)
	}
	before = len(outboxRows(t, db))
	created, err = svc.Stop(t.Context(), created.ID)
	if err != nil || created.Status != exercise.StatusStopped {
		t.Fatalf("stop of a created exercise = %+v, %v", created, err)
	}
	if after := len(outboxRows(t, db)); after != before {
		t.Fatalf("stop of a created exercise emitted %d events", after-before)
	}
	if _, err := svc.Start(t.Context(), created.ID); !errors.Is(err, exercise.ErrConflict) {
		t.Fatalf("start of a stopped exercise = %v, want ErrConflict", err)
	}
}

// Create validates, and the queries report an exercise that does not
// exist.
func TestCreateAndFind(t *testing.T) {
	svc, db := setup(t)
	bad := fixture()
	bad.RoundLimit = 0
	if _, err := svc.Create(t.Context(), bad); !errors.Is(err, exercise.ErrValidation) {
		t.Fatalf("create invalid = %v, want ErrValidation", err)
	}
	ex, err := svc.Create(t.Context(), fixture())
	if err != nil {
		t.Fatal(err)
	}
	if ex.Status != exercise.StatusCreated || ex.RoundInterval != "10ms" || ex.Round != 0 || ex.Verdict != nil ||
		ex.NextRoundAt != nil || ex.Factions != [2]string{"red", "blue"} || len(ex.State.Elements) != 2 || ex.State.Holders == nil {
		t.Fatalf("created = %+v", ex)
	}
	if n := len(outboxRows(t, db)); n != 0 {
		t.Fatalf("create emitted %d events", n)
	}
	h, err := svc.History(t.Context(), ex.ID)
	if err != nil || len(h) != 0 {
		t.Fatalf("history of a created exercise = %v, %v", h, err)
	}
	for _, id := range []string{"019300f0-0000-7000-8000-000000000000", "nope"} {
		if _, err := svc.Find(t.Context(), id); !errors.Is(err, exercise.ErrNotFound) {
			t.Errorf("find %s = %v, want ErrNotFound", id, err)
		}
		if _, err := svc.History(t.Context(), id); !errors.Is(err, exercise.ErrNotFound) {
			t.Errorf("history %s = %v, want ErrNotFound", id, err)
		}
	}
}

// An exercise created without a seed stores one Create drew, and one
// created with a seed keeps it. Without a map and elements, the exercise
// starts from the skirmish its seed lays out.
func TestCreateStoresTheSeed(t *testing.T) {
	svc, db := setup(t)
	seed := func(id string) int64 {
		return scalar[int64](t, db, `SELECT seed FROM exercise WHERE id = $1`, id)
	}
	drawn, err := svc.Create(t.Context(), fixture())
	if err != nil {
		t.Fatal(err)
	}
	if drawn.Seed < 0 || drawn.Seed >= 1<<31 || seed(drawn.ID) != drawn.Seed {
		t.Errorf("drawn seed = %d, stored %d; want one in [0, 2^31), stored", drawn.Seed, seed(drawn.ID))
	}

	c := fixture()
	c.Seed = new(int64(42))
	c.Map, c.Elements = nil, nil
	given, err := svc.Create(t.Context(), c)
	if err != nil {
		t.Fatal(err)
	}
	if given.Seed != 42 || seed(given.ID) != 42 {
		t.Errorf("given seed = %d, stored %d; want 42", given.Seed, seed(given.ID))
	}
	if want := rules.Skirmish(42, c.Factions); !reflect.DeepEqual(given.State, want) {
		t.Errorf("state = %+v, want the skirmish of seed 42 %+v", given.State, want)
	}
	if found := find(t, svc, given.ID); found.Seed != 42 {
		t.Errorf("found seed = %d, want 42", found.Seed)
	}
}
