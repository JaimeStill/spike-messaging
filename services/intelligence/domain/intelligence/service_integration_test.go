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

// openInput is the start of an exercise over one sector "a", 13 by 13,
// whose map carries no objectives.
func openInput(id string) intelligence.Open {
	var m fusion.Map
	if err := json.Unmarshal([]byte(`{"sectors":[{"id":"a","width":13,"height":13,"objectives":[]}]}`), &m); err != nil {
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

// red is red's observation of round: its squad r1 at r1, and the enemy
// contacts it sees.
func red(id string, round int, r1 fusion.Location, contacts ...fusion.Element) intelligence.Observe {
	return intelligence.Observe{Exercise: id, Faction: "red", Observation: fusion.Observation{
		Round:    round,
		Own:      []fusion.Element{{ID: "r1", Faction: "red", Kind: "squad", Strength: 2, Health: []int{2}, Status: "ready", At: r1}},
		Contacts: contacts,
	}}
}

func blue(id string, at fusion.Location) fusion.Element {
	return fusion.Element{ID: id, Faction: "blue", Kind: "squad", Strength: 3, Health: []int{3}, Status: "ready", At: at}
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
		if len(a.Objectives) != 0 {
			t.Errorf("round %d: objectives %+v, want none seen", i, a.Objectives)
		}
		// r1's squad sees the cells around it, clipped at the sector's edge, and
		// each round's are added to the last's.
		if want := []int{4, 10, 12, 14}[i]; len(a.Explored) != want {
			t.Errorf("round %d: %d cells explored, want %d cumulative", i, len(a.Explored), want)
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

// lost is the alert that red lost the objective at 3,3 to blue in round.
func lost(id string, round int) intelligence.Alert {
	return intelligence.Alert{Exercise: id, Faction: "red", Round: round, At: loc(3, 3), Holder: "blue"}
}

// An alert on a picture that covers its round issues a fresh assessment
// carrying the new holder, once; on a picture that lags it, it issues none,
// and the round's observation issues the assessment that carries it.
func TestAlertIssuesOnACurrentPicture(t *testing.T) {
	svc, db := setup(t)
	id := open(t, svc)
	e := event.Event{ID: uuid.NewV7().String(), Source: "/exercise", Type: "exercise.objective.lost"}
	claim := func(ctx context.Context, tx *sqlate.Tx) (bool, error) {
		return in.Claim(ctx, tx, "intelligence-alerts", e)
	}
	if err := svc.Observe(t.Context(), red(id, 0, loc(0, 0)), nil); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := svc.Alert(t.Context(), lost(id, 0), claim); err != nil {
			t.Fatal(err)
		}
	}
	got := assessments(t, db)
	if len(got) != 2 || got[1].Round != 0 || len(got[1].Objectives) != 1 {
		t.Fatalf("outbox holds %+v, want round 0's observed and alerted assessments", got)
	}
	if o := got[1].Objectives[0]; o.At != loc(3, 3) || o.Holder != "blue" || o.Seen != 0 || o.Age != 0 {
		t.Errorf("objective = %+v, want blue's at 3,3 seen in 0", o)
	}
}

func TestAlertOnALaggingPictureWaitsForTheObservation(t *testing.T) {
	svc, db := setup(t)
	id := open(t, svc)
	if err := svc.Alert(t.Context(), lost(id, 1), nil); err != nil {
		t.Fatal(err)
	}
	if got := assessments(t, db); len(got) != 0 {
		t.Fatalf("outbox holds %+v, want none from the alert", got)
	}
	if err := svc.Observe(t.Context(), red(id, 1, loc(0, 0)), nil); err != nil {
		t.Fatal(err)
	}
	got := assessments(t, db)
	if len(got) != 1 || got[0].Round != 1 || len(got[0].Objectives) != 1 {
		t.Fatalf("outbox holds %+v, want round 1's assessment", got)
	}
	if o := got[0].Objectives[0]; o.Holder != "blue" || o.Seen != 1 || o.Age != 0 {
		t.Errorf("objective = %+v, want blue's seen in 1", o)
	}
}

// An alert for an assessment not open yet fails, not permanently, and rolls
// its claim back; one past a closed assessment's last round changes nothing.
func TestAlertBeforeOpenAndAfterClose(t *testing.T) {
	svc, db := setup(t)
	id := uuid.NewV7().String()
	if err := svc.Alert(t.Context(), lost(id, 0), nil); !errors.Is(err, intelligence.ErrNotOpen) || event.IsPermanent(err) {
		t.Errorf("err = %v, want ErrNotOpen, not permanent", err)
	}
	if err := svc.Alert(t.Context(), intelligence.Alert{}, nil); !event.IsPermanent(err) {
		t.Errorf("err = %v, want a permanent validation failure", err)
	}
	if err := svc.Open(t.Context(), openInput(id), nil); err != nil {
		t.Fatal(err)
	}
	if err := svc.Close(t.Context(), intelligence.Close{Exercise: id, Round: 1}, nil); err != nil {
		t.Fatal(err)
	}
	if err := svc.Alert(t.Context(), lost(id, 2), nil); err != nil {
		t.Fatal(err)
	}
	if got := assessments(t, db); len(got) != 0 {
		t.Errorf("outbox holds %+v, want none", got)
	}
	as, err := svc.Find(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if len(as[1].Objectives) != 0 {
		t.Errorf("objectives = %+v, want none", as[1].Objectives)
	}
}

// A conclusion handled before its final round's observation, which exercise
// raises with it, still lets that round be assessed; a closed assessment
// takes no observation of a later round.
func TestCloseStillAssessesTheConcludedRound(t *testing.T) {
	svc, db := setup(t)
	id := open(t, svc)
	if err := svc.Observe(t.Context(), red(id, 1, loc(0, 0)), nil); err != nil {
		t.Fatal(err)
	}
	if err := svc.Close(t.Context(), intelligence.Close{Exercise: id, Round: 2}, nil); err != nil {
		t.Fatal(err)
	}
	for _, round := range []int{2, 3} {
		if err := svc.Observe(t.Context(), red(id, round, loc(0, 0)), nil); err != nil {
			t.Fatal(err)
		}
	}
	got := assessments(t, db)
	if len(got) != 2 || got[1].Round != 2 {
		t.Errorf("outbox holds %+v, want rounds 1 and 2's assessments alone", got)
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
	if as[1].Round != 2 {
		t.Errorf("red's picture is of round %d, want 2", as[1].Round)
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
	if err := m.Down(t.Context(), 2); err != nil {
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
