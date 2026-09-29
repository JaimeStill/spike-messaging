package decide_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/JaimeStill/spike-messaging/services/command/domain/command/decide"
)

func loc(sector string, x, y int) decide.Location {
	return decide.Location{Sector: sector, Point: decide.Point{X: x, Y: y}}
}

func force(id string, strength int, at decide.Location) decide.Element {
	return decide.Element{ID: id, Kind: "force", Strength: strength, At: at}
}

func scout(id string, at decide.Location) decide.Element {
	return decide.Element{ID: id, Kind: "scout", Strength: 1, At: at}
}

func contact(id string, strength int, at decide.Location) decide.Contact {
	return decide.Contact{ID: id, Strength: strength, At: at}
}

func unknown(at decide.Location) decide.Objective { return decide.Objective{At: at} }

func held(by string, at decide.Location) decide.Objective {
	return decide.Objective{At: at, Holder: by, Known: true}
}

// open returns one open w×h sector, a, with the obstacles given.
func open(w, h int, obstacles ...decide.Point) decide.Map {
	return decide.Map{Sectors: []decide.Sector{{ID: "a", Width: w, Height: h, Obstacles: obstacles}}}
}

// summary renders each decision as "element rule [contact] [target]".
func summary(ds []decide.Decision) []string {
	out := []string{}
	for _, d := range ds {
		s := d.Element + " " + string(d.Rule)
		if d.Contact != "" {
			s += " " + d.Contact
		}
		if d.Target != nil {
			s += " " + d.Target.String()
		}
		out = append(out, s)
	}
	return out
}

func check(t *testing.T, got []decide.Decision, want ...string) {
	t.Helper()
	if s := summary(got); !reflect.DeepEqual(s, want) {
		t.Errorf("decisions\n got %q\nwant %q", s, want)
	}
}

// The map and the assessment decode from exercise's and intelligence's
// events' JSON, a location's point flattened beside its sector, and a
// decision encodes an engage's contact and a hold's null target.
func TestDecodesTheEventShapes(t *testing.T) {
	var m decide.Map
	if err := json.Unmarshal([]byte(`{"sectors":[{"id":"a","width":3,"height":1,"obstacles":[{"x":1,"y":0}],
		"objectives":[{"x":2,"y":0}],"gates":[{"at":{"x":0,"y":0},"to":{"sector":"b","x":0,"y":0}}]}]}`), &m); err != nil {
		t.Fatal(err)
	}
	want := decide.Map{Sectors: []decide.Sector{{
		ID: "a", Width: 3, Height: 1, Obstacles: []decide.Point{{X: 1}}, Objectives: []decide.Point{{X: 2}},
		Gates: []decide.Gate{{At: decide.Point{}, To: loc("b", 0, 0)}},
	}}}
	if !reflect.DeepEqual(m, want) {
		t.Errorf("map = %+v", m)
	}
	var a decide.Assessment
	if err := json.Unmarshal([]byte(`{"exercise":"x","faction":"red","round":4,
		"own":[{"id":"r1","faction":"red","kind":"force","strength":3,"at":{"sector":"a","x":0,"y":0}}],
		"contacts":[{"id":"b1","faction":"blue","kind":"scout","strength":1,"at":{"sector":"a","x":1,"y":0},"seen":3,"age":1}],
		"objectives":[{"at":{"sector":"a","x":2,"y":0},"holder":"blue","known":true,"seen":4,"age":0}]}`), &a); err != nil {
		t.Fatal(err)
	}
	wantA := decide.Assessment{
		Round:      4,
		Own:        []decide.Element{force("r1", 3, loc("a", 0, 0))},
		Contacts:   []decide.Contact{contact("b1", 1, loc("a", 1, 0))},
		Objectives: []decide.Objective{held("blue", loc("a", 2, 0))},
	}
	if !reflect.DeepEqual(a, wantA) {
		t.Errorf("assessment = %+v", a)
	}
	target := loc("a", 1, 0)
	out, err := json.Marshal([]decide.Decision{
		{Element: "r1", Rule: decide.Engage, Contact: "b1", Target: &target},
		{Element: "r2", Rule: decide.Hold},
	})
	if err != nil {
		t.Fatal(err)
	}
	const wantJSON = `[{"element":"r1","rule":"engage","contact":"b1","target":{"sector":"a","x":1,"y":0}},` +
		`{"element":"r2","rule":"hold","target":null}]`
	if string(out) != wantJSON {
		t.Errorf("decisions encode as %s", out)
	}
}

// A force engages the nearest weaker contact within three steps, at its
// last-seen cell, before any objective; ties go to the lowest ID.
func TestEngagesTheNearestWeakerContact(t *testing.T) {
	a := decide.Assessment{
		Own: []decide.Element{force("r1", 3, loc("a", 0, 0))},
		Contacts: []decide.Contact{
			contact("b3", 2, loc("a", 3, 0)),
			contact("b2", 1, loc("a", 0, 2)),
			contact("b1", 1, loc("a", 2, 0)),
		},
		Objectives: []decide.Objective{unknown(loc("a", 1, 0))},
	}
	check(t, decide.Decide(open(5, 5), "red", a, nil), "r1 engage b1 a:2,0")
}

// Weaker means strictly weaker: a tie destroys both, so a force does not
// seek one. A contact four steps off is out of reach.
func TestEngagesOnlyAWeakerContactInReach(t *testing.T) {
	a := decide.Assessment{
		Own: []decide.Element{force("r1", 2, loc("a", 0, 0))},
		Contacts: []decide.Contact{
			contact("b1", 2, loc("a", 1, 0)),
			contact("b2", 1, loc("a", 4, 0)),
		},
		Objectives: []decide.Objective{unknown(loc("a", 0, 4))},
	}
	check(t, decide.Decide(open(5, 5), "red", a, nil), "r1 secure a:0,4")
}

// Engaging is for forces: a scout beside a weaker contact secures instead.
func TestScoutsDoNotEngage(t *testing.T) {
	a := decide.Assessment{
		Own:        []decide.Element{{ID: "r1", Kind: "scout", Strength: 5, At: loc("a", 0, 0)}},
		Contacts:   []decide.Contact{contact("b1", 1, loc("a", 1, 0))},
		Objectives: []decide.Objective{unknown(loc("a", 4, 4))},
	}
	check(t, decide.Decide(open(5, 5), "red", a, nil), "r1 secure a:4,4")
}

// Distance is path distance: a contact one cell away across a wall is
// many steps off, and out of reach.
func TestAWallPutsANearContactOutOfReach(t *testing.T) {
	// Column x=1 is a wall but for its last cell.
	m := open(3, 6, decide.Point{X: 1, Y: 0}, decide.Point{X: 1, Y: 1}, decide.Point{X: 1, Y: 2},
		decide.Point{X: 1, Y: 3}, decide.Point{X: 1, Y: 4})
	a := decide.Assessment{
		Own:        []decide.Element{force("r1", 3, loc("a", 0, 0))},
		Contacts:   []decide.Contact{contact("b1", 1, loc("a", 2, 0))},
		Objectives: []decide.Objective{unknown(loc("a", 0, 3))},
	}
	check(t, decide.Decide(m, "red", a, nil), "r1 secure a:0,3")
}

// An element secures the nearest objective its faction does not hold. An
// unknown objective is unheld whatever its holder says, and one it
// believes it holds is left, however stale the belief.
func TestSecuresTheNearestObjectiveNotHeld(t *testing.T) {
	a := decide.Assessment{
		Own: []decide.Element{scout("r1", loc("a", 0, 0))},
		Objectives: []decide.Objective{
			held("red", loc("a", 1, 0)),
			{At: loc("a", 2, 0), Holder: "red"}, // unknown: its holder means nothing
			held("blue", loc("a", 0, 1)),
		},
	}
	check(t, decide.Decide(open(5, 5), "red", a, nil), "r1 secure a:0,1")

	a.Objectives = []decide.Objective{held("red", loc("a", 1, 0)), {At: loc("a", 2, 0), Holder: "red"}}
	check(t, decide.Decide(open(5, 5), "red", a, nil), "r1 secure a:2,0")
}

// No two elements secure one objective: each takes the nearest none
// before it in ID order took, and one with none left holds.
func TestSecuresEachObjectiveOnce(t *testing.T) {
	a := decide.Assessment{
		Own: []decide.Element{
			scout("r3", loc("a", 4, 4)),
			scout("r1", loc("a", 0, 0)),
			scout("r2", loc("a", 1, 0)),
		},
		Objectives: []decide.Objective{unknown(loc("a", 2, 0)), unknown(loc("a", 4, 0))},
	}
	check(t, decide.Decide(open(5, 5), "red", a, nil),
		"r1 secure a:2,0", "r2 secure a:4,0", "r3 hold")
}

// An element keeps the objective it was securing, though another is now
// nearer to it, and its standing target is claimed before any element
// picks: here r1 would otherwise take r2's.
func TestKeepsAStandingTarget(t *testing.T) {
	a := decide.Assessment{
		Own:        []decide.Element{scout("r1", loc("a", 3, 0)), scout("r2", loc("a", 0, 4))},
		Objectives: []decide.Objective{unknown(loc("a", 4, 0)), unknown(loc("a", 0, 0))},
	}
	far, near := loc("a", 0, 0), loc("a", 4, 0)
	standing := []decide.Decision{
		{Element: "r1", Rule: decide.Secure, Target: &far},
		{Element: "r2", Rule: decide.Secure, Target: &near},
	}
	check(t, decide.Decide(open(5, 5), "red", a, standing), "r1 secure a:0,0", "r2 secure a:4,0")

	// Once r1's objective is held, it picks again.
	a.Objectives[1] = held("red", loc("a", 0, 0))
	check(t, decide.Decide(open(5, 5), "red", a, standing), "r1 hold", "r2 secure a:4,0")
}

// An engage outranks a standing target, and a standing target the element
// can no longer reach is dropped.
func TestAStandingTargetYields(t *testing.T) {
	target := loc("a", 4, 4)
	standing := []decide.Decision{{Element: "r1", Rule: decide.Secure, Target: &target}}
	a := decide.Assessment{
		Own:        []decide.Element{force("r1", 3, loc("a", 0, 0))},
		Contacts:   []decide.Contact{contact("b1", 1, loc("a", 0, 1))},
		Objectives: []decide.Objective{unknown(target)},
	}
	check(t, decide.Decide(open(5, 5), "red", a, standing), "r1 engage b1 a:0,1")

	// Walled into its corner, r1 reaches nothing.
	a.Contacts = nil
	walled := open(5, 5, decide.Point{X: 1, Y: 0}, decide.Point{X: 0, Y: 1})
	check(t, decide.Decide(walled, "red", a, standing), "r1 hold")
}

// A path runs through a gate: entering the gate's cell and traversing it
// are separate steps, so a contact just past a gate is two steps off.
func TestReachesAcrossAGate(t *testing.T) {
	m := decide.Map{Sectors: []decide.Sector{
		{ID: "a", Width: 2, Height: 1, Gates: []decide.Gate{{At: decide.Point{X: 1}, To: loc("b", 0, 0)}}},
		{ID: "b", Width: 4, Height: 1, Gates: []decide.Gate{{At: decide.Point{X: 0}, To: loc("a", 1, 0)}}},
	}}
	a := decide.Assessment{
		Own:        []decide.Element{force("r1", 3, loc("a", 0, 0))},
		Contacts:   []decide.Contact{contact("b1", 1, loc("b", 1, 0))},
		Objectives: []decide.Objective{unknown(loc("b", 3, 0))},
	}
	// a:0,0 → a:1,0 → b:0,0 → b:1,0 is three steps: in reach.
	check(t, decide.Decide(m, "red", a, nil), "r1 engage b1 b:1,0")

	a.Contacts = nil
	check(t, decide.Decide(m, "red", a, nil), "r1 secure b:3,0")
}

// An objective no step sequence reaches is never chosen.
func TestIgnoresAnUnreachableObjective(t *testing.T) {
	m := open(3, 3, decide.Point{X: 1, Y: 2}, decide.Point{X: 2, Y: 1})
	a := decide.Assessment{
		Own:        []decide.Element{scout("r1", loc("a", 0, 0))},
		Objectives: []decide.Objective{unknown(loc("a", 2, 2))},
	}
	check(t, decide.Decide(m, "red", a, nil), "r1 hold")
}

// Every decision is in ID order, one per own element, and an assessment
// with none decides nothing.
func TestDecidesForEveryElement(t *testing.T) {
	if got := decide.Decide(open(1, 1), "red", decide.Assessment{}, nil); len(got) != 0 {
		t.Errorf("no elements: %v", got)
	}
	a := decide.Assessment{Own: []decide.Element{scout("r2", loc("a", 0, 0)), scout("r1", loc("a", 0, 0))}}
	if got := strings.Join(summary(decide.Decide(open(1, 1), "red", a, nil)), ", "); got != "r1 hold, r2 hold" {
		t.Errorf("decisions = %s", got)
	}
}
