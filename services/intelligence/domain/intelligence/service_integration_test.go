//go:build integration

package intelligence_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"uuid"

	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/migrate"

	"github.com/JaimeStill/spike-messaging/core/event"
	"github.com/JaimeStill/spike-messaging/messaging/inbox"
	"github.com/JaimeStill/spike-messaging/messaging/outbox"
	"github.com/JaimeStill/spike-messaging/messaging/postgres"
	"github.com/JaimeStill/spike-messaging/services/intelligence/data"
	"github.com/JaimeStill/spike-messaging/services/intelligence/domain/intelligence"
	"github.com/JaimeStill/spike-messaging/services/intelligence/domain/intelligence/fusion"
	"github.com/JaimeStill/spike-messaging/services/intelligence/internal/pgtest"
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

// contactRounds is the K the tests run under.
const contactRounds = 2

// setup returns the service over a throwaway, migrated database, with the
// real recorder over the outbox's sink, and the database for the test's
// own reads.
func setup(t *testing.T) (*intelligence.Service, *data.Database) {
	t.Helper()
	db := pgtest.Open(t)
	svc := intelligence.New(db, event.NewRecorder(ob.Sink(), "/intelligence"), contactRounds)
	if err := svc.Verify(t.Context()); err != nil {
		t.Fatalf("verify: %v", err)
	}
	return svc, db
}

func loc(x, y int) fusion.Location {
	return fusion.Location{Sector: "a", Point: fusion.Point{X: x, Y: y}}
}

// openInput is the start of an exercise over one sector "a" with its
// objective at 4,0.
func openInput(id string) intelligence.Open {
	var m fusion.Map
	if err := json.Unmarshal([]byte(`{"sectors":[{"id":"a","objectives":[{"x":4,"y":0}]}]}`), &m); err != nil {
		panic(err)
	}
	return intelligence.Open{Exercise: id, Map: m, Factions: [2]string{"red", "blue"}}
}

func open(t *testing.T, svc *intelligence.Service) string {
	t.Helper()
	id := uuid.NewV7().String()
	if err := svc.Open(t.Context(), openInput(id), nil); err != nil {
		t.Fatalf("open: %v", err)
	}
	return id
}

// red is red's observation of round: its force r1 at r1, and the enemy
// contacts it sees.
func red(id string, round int, r1 fusion.Location, contacts ...fusion.Element) intelligence.Observe {
	return intelligence.Observe{Exercise: id, Faction: "red", Observation: fusion.Observation{
		Round:    round,
		Own:      []fusion.Element{{ID: "r1", Faction: "red", Kind: "force", Strength: 2, At: r1}},
		Contacts: contacts,
	}}
}

func blue(id string, at fusion.Location) fusion.Element {
	return fusion.Element{ID: id, Faction: "blue", Kind: "force", Strength: 3, At: at}
}

// assessments returns the assessments the outbox holds, in the order they
// were written.
func assessments(t *testing.T, db *data.Database) []intelligence.AssessmentData {
	t.Helper()
	r, err := db.QueryContext(t.Context(), `SELECT header, data FROM messaging_outbox ORDER BY seq`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	var out []intelligence.AssessmentData
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
			t.Fatal(err)
		}
		if e.Type != intelligence.AssessmentIssued.Type() {
			t.Fatalf("outbox holds a %s event", e.Type)
		}
		var d intelligence.AssessmentData
		if err := json.Unmarshal(e.Data, &d); err != nil {
			t.Fatal(err)
		}
		if e.Subject != d.Exercise {
			t.Errorf("subject %s, want the exercise %s", e.Subject, d.Exercise)
		}
		out = append(out, d)
	}
	if err := r.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// Each observed round issues the faction's assessment. A contact seen in
// round 0 stays at its last-seen cell, aging, while r1 moves out of sight,
// and drops once it has gone more than K rounds unseen.
func TestObserveIssuesAssessments(t *testing.T) {
	svc, db := setup(t)
	id := open(t, svc)
	ctx := t.Context()
	inputs := []intelligence.Observe{
		red(id, 0, loc(0, 0), blue("b1", loc(2, 0))),
		red(id, 1, loc(0, 3)),
		red(id, 2, loc(0, 4)),
		red(id, 3, loc(0, 5)),
	}
	for _, c := range inputs {
		if err := svc.Observe(ctx, c, nil); err != nil {
			t.Fatal(err)
		}
	}
	got := assessments(t, db)
	if len(got) != 4 {
		t.Fatalf("outbox holds %d assessments, want 4", len(got))
	}
	wantAges := [][]int{{0}, {1}, {2}, {}}
	for i, a := range got {
		if a.Round != i || a.Faction != "red" {
			t.Errorf("assessment %d is %s's of round %d", i, a.Faction, a.Round)
		}
		var ages []int
		for _, c := range a.Contacts {
			if c.ID != "b1" || c.At != loc(2, 0) || c.Seen != 0 {
				t.Errorf("round %d: contact %+v, want b1 at its last-seen cell", i, c)
			}
			ages = append(ages, c.Age)
		}
		if len(ages) != len(wantAges[i]) || (len(ages) == 1 && ages[0] != wantAges[i][0]) {
			t.Errorf("round %d: contact ages %v, want %v", i, ages, wantAges[i])
		}
		if len(a.Objectives) != 1 || a.Objectives[0].Known {
			t.Errorf("round %d: objectives %+v, want the one objective unknown", i, a.Objectives)
		}
	}

	as, err := svc.Find(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(as) != 2 || as[1].Faction != "red" || as[1].Round != 3 || as[0].Round != -1 {
		t.Errorf("Find = %+v", as)
	}
}

// An observation of a round the assessment already covers changes nothing.
func TestStaleObservationsAreSkipped(t *testing.T) {
	svc, db := setup(t)
	id := open(t, svc)
	for _, round := range []int{1, 0, 1} {
		if err := svc.Observe(t.Context(), red(id, round, loc(0, 0)), nil); err != nil {
			t.Fatal(err)
		}
	}
	if got := assessments(t, db); len(got) != 1 || got[0].Round != 1 {
		t.Errorf("outbox holds %+v, want round 1's assessment alone", got)
	}
}

// An input for an assessment not open yet fails, not permanently, and rolls
// its claim back, so its redelivery after the start is handled.
func TestInputsBeforeOpenAreRedelivered(t *testing.T) {
	svc, db := setup(t)
	id := uuid.NewV7().String()
	e := event.Event{ID: uuid.NewV7().String(), Source: "/exercise", Type: "exercise.round.observed"}
	claim := func(ctx context.Context, tx *sqlate.Tx) (bool, error) {
		return in.Claim(ctx, tx, "intelligence-observed", e)
	}

	for _, err := range []error{
		svc.Observe(t.Context(), red(id, 0, loc(0, 0)), claim),
		svc.Close(t.Context(), intelligence.Close{Exercise: id}, nil),
	} {
		if !errors.Is(err, intelligence.ErrNotOpen) || event.IsPermanent(err) {
			t.Errorf("err = %v, want ErrNotOpen, not permanent", err)
		}
	}
	if _, err := svc.Find(t.Context(), id); !errors.Is(err, intelligence.ErrNotFound) {
		t.Errorf("Find = %v, want ErrNotFound", err)
	}

	if err := svc.Open(t.Context(), openInput(id), nil); err != nil {
		t.Fatal(err)
	}
	if err := svc.Observe(t.Context(), red(id, 0, loc(0, 0)), claim); err != nil {
		t.Fatal(err)
	}
	if got := assessments(t, db); len(got) != 1 {
		t.Errorf("outbox holds %+v, want the redelivered observation's assessment", got)
	}
}

// A redelivered event, claimed again, changes nothing.
func TestAClaimedRepeatChangesNothing(t *testing.T) {
	svc, db := setup(t)
	id := open(t, svc)
	e := event.Event{ID: uuid.NewV7().String(), Source: "/exercise", Type: "exercise.round.observed"}
	claim := func(ctx context.Context, tx *sqlate.Tx) (bool, error) {
		return in.Claim(ctx, tx, "intelligence-observed", e)
	}
	for range 2 {
		if err := svc.Observe(t.Context(), red(id, 0, loc(0, 0)), claim); err != nil {
			t.Fatal(err)
		}
		// A repeat of a round would be skipped anyway; the claim must stop
		// it first, so reset the round the assessment covers.
		if _, err := db.ExecContext(t.Context(), `UPDATE assessment SET picture = jsonb_set(picture, '{round}', '-1')`); err != nil {
			t.Fatal(err)
		}
	}
	if got := assessments(t, db); len(got) != 1 {
		t.Errorf("outbox holds %d assessments, want 1", len(got))
	}
}

// A closed assessment takes no more observations.
func TestCloseEndsTheAssessment(t *testing.T) {
	svc, db := setup(t)
	id := open(t, svc)
	if err := svc.Close(t.Context(), intelligence.Close{Exercise: id}, nil); err != nil {
		t.Fatal(err)
	}
	if err := svc.Observe(t.Context(), red(id, 0, loc(0, 0)), nil); err != nil {
		t.Fatal(err)
	}
	if got := assessments(t, db); len(got) != 0 {
		t.Errorf("a closed assessment issued %+v", got)
	}
	as, err := svc.Find(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range as {
		if a.Status != intelligence.StatusClosed {
			t.Errorf("%s is %s", a.Faction, a.Status)
		}
	}
}

// Two replicas handling one round's observation at once assess it once:
// the row lock orders them, and the second skips the round.
func TestConcurrentObservationsIssueOnce(t *testing.T) {
	svc, db := setup(t)
	id := open(t, svc)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Go(func() { errs <- svc.Observe(context.Background(), red(id, 0, loc(0, 0)), nil) })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := assessments(t, db); len(got) != 1 {
		t.Errorf("outbox holds %d assessments, want 1", len(got))
	}
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
		t.Fatalf("down: %v", err)
	}
	r, err := db.QueryContext(t.Context(), `SELECT to_regclass('assessment') IS NOT NULL`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	var exists bool
	if !r.Next() || r.Scan(&exists) != nil || exists {
		t.Error("after down, the assessment table remains")
	}
}
