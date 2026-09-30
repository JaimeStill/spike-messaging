//go:build integration

package command_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"uuid"

	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/migrate"

	"github.com/JaimeStill/spike-messaging/core/event"
	"github.com/JaimeStill/spike-messaging/messaging/inbox"
	"github.com/JaimeStill/spike-messaging/messaging/outbox"
	"github.com/JaimeStill/spike-messaging/messaging/postgres"
	"github.com/JaimeStill/spike-messaging/services/command/data"
	"github.com/JaimeStill/spike-messaging/services/command/domain/command"
	"github.com/JaimeStill/spike-messaging/services/command/domain/command/decide"
	"github.com/JaimeStill/spike-messaging/services/command/internal/pgtest"
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
func setup(t *testing.T) (*command.Service, *data.Database) {
	t.Helper()
	db := pgtest.Open(t)
	svc := command.New(db, event.NewRecorder(ob.Sink(), "/command"))
	if err := svc.Verify(t.Context()); err != nil {
		t.Fatalf("verify: %v", err)
	}
	return svc, db
}

func loc(x, y int) decide.Location {
	return decide.Location{Sector: "a", Point: decide.Point{X: x, Y: y}}
}

// openInput is the start of an exercise over one open 5×3 sector "a",
// its objectives hidden. red's assessments place one at 4,0.
func openInput(id string) command.Open {
	var m decide.Map
	if err := json.Unmarshal([]byte(`{"sectors":[{"id":"a","width":5,"height":3,"obstacles":[],
		"objectives":[],"gates":[]}]}`), &m); err != nil {
		panic(err)
	}
	return command.Open{Exercise: id, Map: m, Factions: [2]string{"red", "blue"}}
}

func open(t *testing.T, svc *command.Service) string {
	t.Helper()
	id := uuid.NewV7().String()
	if err := svc.Open(t.Context(), openInput(id), nil); err != nil {
		t.Fatalf("open: %v", err)
	}
	return id
}

// squad is a ready squad at full strength at at.
func squad(id string, at decide.Location) decide.Element {
	return decide.Element{ID: id, Kind: decide.Squad, Strength: 400, Health: []int{100, 100, 100, 100},
		Status: decide.Ready, At: at}
}

// explored is every cell of openInput's sector, so no element searches.
var explored = func() []decide.Location {
	var out []decide.Location
	for y := range 3 {
		for x := range 5 {
			out = append(out, loc(x, y))
		}
	}
	return out
}()

// red is red's assessment of round: its squad r1 at 0,0 and its scout r2
// at 0,2, the objective as given, the contacts it knows, and the whole
// sector explored.
func red(id string, round int, objective decide.Objective, contacts ...decide.Contact) command.Decide {
	return command.Decide{Exercise: id, Faction: "red", Assessment: decide.Assessment{
		Round: round,
		Own: []decide.Element{
			squad("r1", loc(0, 0)),
			{ID: "r2", Kind: decide.Scout, Strength: 100, Health: []int{100}, Status: decide.Ready, At: loc(0, 2)},
		},
		Contacts:   contacts,
		Objectives: []decide.Objective{objective},
		Explored:   explored,
	}}
}

var unknown = decide.Objective{At: loc(4, 0)}

// directives returns the directives the outbox holds, in the order they
// were written.
func directives(t *testing.T, db *data.Database) []command.DirectiveData {
	t.Helper()
	r, err := db.QueryContext(t.Context(), `SELECT header, data FROM messaging_outbox ORDER BY seq`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	var out []command.DirectiveData
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
		if e.Type != command.DirectiveIssued.Type() {
			t.Fatalf("outbox holds a %s event", e.Type)
		}
		var d command.DirectiveData
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

// summary renders a directive's decisions as "element rule [contact]
// [target]".
func summary(d command.DirectiveData) string {
	var parts []string
	for _, x := range d.Directives {
		s := x.Element + " " + string(x.Rule)
		if x.Contact != "" {
			s += " " + x.Contact
		}
		if x.Target != nil {
			s += " " + x.Target.String()
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, ", ")
}

// A directive is issued only on a round whose decisions send an element
// somewhere new, and it lists every live element. r1 secures the objective
// and r2, with nothing else to do, rescouts it; an unchanged round issues
// nothing; r1 engages a weak
// contact and r2 takes the objective over; once the objective is known to
// be red's and the contact is gone, both hold.
func TestDecideIssuesDirectivesOnChange(t *testing.T) {
	svc, db := setup(t)
	id := open(t, svc)
	ctx := t.Context()
	b1 := decide.Contact{ID: "b1", Kind: decide.Scout, Strength: 100, At: loc(2, 0)}
	for _, c := range []command.Decide{
		red(id, 0, unknown),
		red(id, 1, unknown),
		red(id, 2, unknown, b1),
		red(id, 3, decide.Objective{At: loc(4, 0), Holder: "red", Known: true}),
	} {
		if err := svc.Decide(ctx, c, nil); err != nil {
			t.Fatal(err)
		}
	}
	got := directives(t, db)
	want := []struct {
		round int
		text  string
	}{
		{0, "r1 secure a:4,0, r2 rescout a:4,0"},
		{2, "r1 engage b1 a:2,0, r2 secure a:4,0"},
		{3, "r1 hold, r2 hold"},
	}
	if len(got) != len(want) {
		t.Fatalf("outbox holds %d directives, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Round != w.round || got[i].Faction != "red" || summary(got[i]) != w.text {
			t.Errorf("directive %d is %s's of round %d: %s; want round %d: %s",
				i, got[i].Faction, got[i].Round, summary(got[i]), w.round, w.text)
		}
	}

	ds, err := svc.Find(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(ds) != 2 || ds[0].Faction != "blue" || ds[0].Round != -1 || len(ds[0].Decisions) != 0 ||
		ds[1].Faction != "red" || ds[1].Round != 3 || len(ds[1].Decisions) != 2 {
		t.Errorf("Find = %+v", ds)
	}
}

// A changed rule issues a directive though no target changes. r1
// reinforces r3's fight, which r3 pursues; once r3 is gone, r1 engages the
// same contact in the same cell, and the round after, unchanged, issues
// nothing.
func TestDecideIssuesDirectivesOnARuleChange(t *testing.T) {
	svc, db := setup(t)
	id := open(t, svc)
	b1 := decide.Contact{ID: "b1", Kind: decide.Scout, Strength: 100, At: loc(2, 0)}
	r3 := squad("r3", loc(2, 0))
	r3.Status = decide.Engaged
	for round, own := range [][]decide.Element{
		{squad("r1", loc(0, 0)), r3},
		{squad("r1", loc(0, 0))},
		{squad("r1", loc(0, 0))},
	} {
		c := command.Decide{Exercise: id, Faction: "red", Assessment: decide.Assessment{
			Round: round, Own: own, Contacts: []decide.Contact{b1}, Objectives: []decide.Objective{unknown},
			Explored: explored,
		}}
		if err := svc.Decide(t.Context(), c, nil); err != nil {
			t.Fatal(err)
		}
	}
	got := directives(t, db)
	want := []string{"r1 reinforce b1 a:2,0, r3 pursue b1 a:2,0", "r1 engage b1 a:2,0"}
	if len(got) != len(want) {
		t.Fatalf("outbox holds %d directives, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Round != i || summary(got[i]) != w {
			t.Errorf("directive %d is round %d's: %s; want round %d: %s", i, got[i].Round, summary(got[i]), i, w)
		}
	}
}

// An assessment of a round before the one the direction last decided on
// changes nothing, and a repeat of that round decides the same.
func TestStaleAssessmentsAreSkipped(t *testing.T) {
	svc, db := setup(t)
	id := open(t, svc)
	for _, round := range []int{1, 0, 1} {
		if err := svc.Decide(t.Context(), red(id, round, unknown), nil); err != nil {
			t.Fatal(err)
		}
	}
	if got := directives(t, db); len(got) != 1 || got[0].Round != 1 {
		t.Errorf("outbox holds %+v, want round 1's directive alone", got)
	}
}

// An input for a direction not open yet fails, not permanently, and rolls
// its claim back, so its redelivery after the start is handled.
func TestInputsBeforeOpenAreRedelivered(t *testing.T) {
	svc, db := setup(t)
	id := uuid.NewV7().String()
	e := event.Event{ID: uuid.NewV7().String(), Source: "/intelligence", Type: "intelligence.assessment.issued"}
	claim := func(ctx context.Context, tx *sqlate.Tx) (bool, error) {
		return in.Claim(ctx, tx, "command-assessed", e)
	}

	for _, err := range []error{
		svc.Decide(t.Context(), red(id, 0, unknown), claim),
		svc.Close(t.Context(), command.Close{Exercise: id}, nil),
	} {
		if !errors.Is(err, command.ErrNotOpen) || event.IsPermanent(err) {
			t.Errorf("err = %v, want ErrNotOpen, not permanent", err)
		}
	}
	if _, err := svc.Find(t.Context(), id); !errors.Is(err, command.ErrNotFound) {
		t.Errorf("Find = %v, want ErrNotFound", err)
	}

	if err := svc.Open(t.Context(), openInput(id), nil); err != nil {
		t.Fatal(err)
	}
	if err := svc.Decide(t.Context(), red(id, 0, unknown), claim); err != nil {
		t.Fatal(err)
	}
	if got := directives(t, db); len(got) != 1 {
		t.Errorf("outbox holds %+v, want the redelivered assessment's directive", got)
	}
}

// A redelivered event, claimed again, changes nothing.
func TestAClaimedRepeatChangesNothing(t *testing.T) {
	svc, db := setup(t)
	id := open(t, svc)
	e := event.Event{ID: uuid.NewV7().String(), Source: "/intelligence", Type: "intelligence.assessment.issued"}
	claim := func(ctx context.Context, tx *sqlate.Tx) (bool, error) {
		return in.Claim(ctx, tx, "command-assessed", e)
	}
	for range 2 {
		if err := svc.Decide(t.Context(), red(id, 0, unknown), claim); err != nil {
			t.Fatal(err)
		}
		// A repeat of a round would change no target anyway; the claim
		// must stop it first, so reset the direction.
		if _, err := db.ExecContext(t.Context(), `UPDATE direction SET round = -1, decisions = '[]'`); err != nil {
			t.Fatal(err)
		}
	}
	if got := directives(t, db); len(got) != 1 {
		t.Errorf("outbox holds %d directives, want 1", len(got))
	}
}

// A revised assessment of the round last decided on is decided on again:
// once red learns blue has taken the objective it held, both elements head
// for it, and a repeat of the revision issues nothing.
func TestARevisedAssessmentIsDecidedAgain(t *testing.T) {
	svc, db := setup(t)
	id := open(t, svc)
	for _, c := range []command.Decide{
		red(id, 1, decide.Objective{At: loc(4, 0), Holder: "red", Known: true}),
		red(id, 1, decide.Objective{At: loc(4, 0), Holder: "blue", Known: true}),
		red(id, 1, decide.Objective{At: loc(4, 0), Holder: "blue", Known: true}),
	} {
		if err := svc.Decide(t.Context(), c, nil); err != nil {
			t.Fatal(err)
		}
	}
	got := directives(t, db)
	if len(got) != 1 || got[0].Round != 1 || summary(got[0]) != "r1 secure a:4,0, r2 rescout a:4,0" {
		t.Errorf("outbox holds %+v, want the revision's directive alone", got)
	}
}

// A conclusion handled before its final round's assessment still lets that
// round be decided on; a closed direction takes no assessment of a later
// round.
func TestCloseStillDecidesTheConcludedRound(t *testing.T) {
	svc, db := setup(t)
	id := open(t, svc)
	if err := svc.Decide(t.Context(), red(id, 1, unknown), nil); err != nil {
		t.Fatal(err)
	}
	if err := svc.Close(t.Context(), command.Close{Exercise: id, Round: 2}, nil); err != nil {
		t.Fatal(err)
	}
	b1 := decide.Contact{ID: "b1", Kind: decide.Scout, Strength: 100, At: loc(2, 0)}
	for _, round := range []int{2, 3} {
		if err := svc.Decide(t.Context(), red(id, round, unknown, b1), nil); err != nil {
			t.Fatal(err)
		}
	}
	got := directives(t, db)
	if len(got) != 2 || got[1].Round != 2 {
		t.Errorf("outbox holds %+v, want rounds 1 and 2's directives alone", got)
	}
	ds, err := svc.Find(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range ds {
		if d.Status != command.StatusClosed {
			t.Errorf("%s is %s", d.Faction, d.Status)
		}
	}
	if ds[1].Round != 2 {
		t.Errorf("red decided on round %d, want 2", ds[1].Round)
	}
}

// Two replicas handling one round's assessment at once issue one
// directive: the row lock orders them, and the second decides the same.
func TestConcurrentAssessmentsIssueOnce(t *testing.T) {
	svc, db := setup(t)
	id := open(t, svc)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Go(func() { errs <- svc.Decide(context.Background(), red(id, 0, unknown), nil) })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := directives(t, db); len(got) != 1 {
		t.Errorf("outbox holds %d directives, want 1", len(got))
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
	r, err := db.QueryContext(t.Context(), `SELECT to_regclass('direction') IS NOT NULL`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	var exists bool
	if !r.Next() || r.Scan(&exists) != nil || exists {
		t.Error("after down, the direction table remains")
	}
}
