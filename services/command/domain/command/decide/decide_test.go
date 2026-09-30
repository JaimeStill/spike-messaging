package decide_test

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/JaimeStill/spike-messaging/services/command/domain/command/decide"
)

func loc(sector string, x, y int) decide.Location {
	return decide.Location{Sector: sector, Point: decide.Point{X: x, Y: y}}
}

// squad returns a ready squad of the operators' health given, four at
// full health when none is.
func squad(id string, at decide.Location, health ...int) decide.Element {
	if len(health) == 0 {
		health = []int{100, 100, 100, 100}
	}
	strength := 0
	for _, h := range health {
		strength += h
	}
	return decide.Element{ID: id, Kind: decide.Squad, Strength: strength, Health: health, Status: decide.Ready, At: at}
}

func scout(id string, at decide.Location) decide.Element {
	return decide.Element{ID: id, Kind: decide.Scout, Strength: 100, Health: []int{100}, Status: decide.Ready, At: at}
}

// engaged returns e with the engaged status.
func engaged(e decide.Element) decide.Element {
	e.Status = decide.Engaged
	return e
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

// explored returns every cell of m's sectors but those given, as an
// assessment's explored cells.
func explored(m decide.Map, except ...decide.Location) []decide.Location {
	var out []decide.Location
	for _, s := range m.Sectors {
		for y := range s.Height {
			for x := range s.Width {
				if l := loc(s.ID, x, y); !slices.Contains(except, l) {
					out = append(out, l)
				}
			}
		}
	}
	return out
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
// decision encodes a fight's or a retreat's contact and a hold's null
// target.
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
		"own":[{"id":"r1","faction":"red","kind":"squad","strength":287,"health":[100,100,87],"status":"engaged",
			"at":{"sector":"a","x":0,"y":0}}],
		"contacts":[{"id":"b1","faction":"blue","kind":"scout","strength":100,"health":[100],"status":"recovering",
			"at":{"sector":"a","x":1,"y":0},"seen":3,"age":1}],
		"objectives":[{"at":{"sector":"a","x":2,"y":0},"holder":"blue","known":true,"seen":2,"age":2}],
		"explored":[{"sector":"a","x":0,"y":0},{"sector":"a","x":2,"y":0}]}`), &a); err != nil {
		t.Fatal(err)
	}
	wantA := decide.Assessment{
		Round: 4,
		Own:   []decide.Element{engaged(squad("r1", loc("a", 0, 0), 100, 100, 87))},
		Contacts: []decide.Contact{{ID: "b1", Kind: decide.Scout, Strength: 100, Status: decide.Recovering,
			At: loc("a", 1, 0), Age: 1}},
		Objectives: []decide.Objective{stale(held("blue", loc("a", 2, 0)), 2)},
		Explored:   []decide.Location{loc("a", 0, 0), loc("a", 2, 0)},
	}
	if !reflect.DeepEqual(a, wantA) {
		t.Errorf("assessment = %+v", a)
	}
	target := loc("a", 1, 0)
	out, err := json.Marshal([]decide.Decision{
		{Element: "r1", Rule: decide.Engage, Contact: "b1", Target: &target},
		{Element: "r2", Rule: decide.Hold},
		{Element: "r3", Rule: decide.Retreat, Contact: "b1", Target: &target},
		{Element: "r4", Rule: decide.Pursue, Contact: "b1", Target: &target},
		{Element: "r5", Rule: decide.Search, Target: &target},
		{Element: "r6", Rule: decide.Rescout, Target: &target},
	})
	if err != nil {
		t.Fatal(err)
	}
	const wantJSON = `[{"element":"r1","rule":"engage","contact":"b1","target":{"sector":"a","x":1,"y":0}},` +
		`{"element":"r2","rule":"hold","target":null},` +
		`{"element":"r3","rule":"retreat","contact":"b1","target":{"sector":"a","x":1,"y":0}},` +
		`{"element":"r4","rule":"pursue","contact":"b1","target":{"sector":"a","x":1,"y":0}},` +
		`{"element":"r5","rule":"search","target":{"sector":"a","x":1,"y":0}},` +
		`{"element":"r6","rule":"rescout","target":{"sector":"a","x":1,"y":0}}]`
	if string(out) != wantJSON {
		t.Errorf("decisions encode as %s", out)
	}
}

// A squad engages the nearest weaker contact within three steps, at its
// last-seen cell, before any objective; ties go to the lowest ID.
func TestEngagesTheNearestWeakerContact(t *testing.T) {
	a := decide.Assessment{
		Own: []decide.Element{squad("r1", loc("a", 0, 0))},
		Contacts: []decide.Contact{
			contact("b3", 300, loc("a", 3, 0)),
			contact("b2", 100, loc("a", 0, 2)),
			contact("b1", 100, loc("a", 2, 0)),
		},
		Objectives: []decide.Objective{unknown(loc("a", 1, 0))},
	}
	check(t, decide.Decide(open(5, 5), "red", a, nil), "r1 engage b1 a:2,0")
}

// Weaker means strictly weaker: an even fight wears both sides down alike,
// so a squad does not seek one. A contact four steps off is out of reach.
func TestEngagesOnlyAWeakerContactInReach(t *testing.T) {
	a := decide.Assessment{
		Own: []decide.Element{squad("r1", loc("a", 0, 0), 100, 100)},
		Contacts: []decide.Contact{
			contact("b1", 200, loc("a", 1, 0)),
			contact("b2", 100, loc("a", 4, 0)),
		},
		Objectives: []decide.Objective{unknown(loc("a", 0, 4))},
	}
	check(t, decide.Decide(open(5, 5), "red", a, nil), "r1 secure a:0,4")
}

// Engaging is for squads: a scout beside a weaker contact secures instead.
func TestScoutsDoNotEngage(t *testing.T) {
	a := decide.Assessment{
		Own:        []decide.Element{scout("r1", loc("a", 0, 0))},
		Contacts:   []decide.Contact{contact("b1", 50, loc("a", 1, 0))},
		Objectives: []decide.Objective{unknown(loc("a", 4, 4))},
		Explored:   explored(open(5, 5)),
	}
	check(t, decide.Decide(open(5, 5), "red", a, nil), "r1 secure a:4,4")
}

// An engaged squad whose faction has less than two thirds of the enemy's
// strength in its cell retreats one step, to the open neighbor farthest from the
// nearest contact outside its fight, leaving the strongest enemy there.
func TestAWeakEngagedSquadRetreats(t *testing.T) {
	fight := loc("a", 2, 2)
	a := decide.Assessment{
		Own: []decide.Element{engaged(squad("r1", fight, 60, 40))},
		Contacts: []decide.Contact{
			contact("b2", 150, fight),
			contact("b1", 150, fight),
			{ID: "b3", Strength: 400, At: loc("a", 4, 2), Age: 1},
		},
	}
	check(t, decide.Decide(open(5, 5), "red", a, nil), "r1 retreat b1 a:1,2")

	// A neighbor holding a contact seen this round is no way out, and a
	// tie between the rest goes to the first of up, right, down, left.
	a.Contacts = append(a.Contacts, contact("b4", 50, loc("a", 1, 2)))
	check(t, decide.Decide(open(5, 5), "red", a, nil), "r1 retreat b1 a:2,1")
}

// A scout in a fight always retreats, however weak the enemy there.
func TestAnEngagedScoutRetreats(t *testing.T) {
	a := decide.Assessment{
		Own:      []decide.Element{engaged(scout("r1", loc("a", 0, 0)))},
		Contacts: []decide.Contact{contact("b1", 10, loc("a", 0, 0))},
	}
	check(t, decide.Decide(open(5, 5), "red", a, nil), "r1 retreat b1 a:1,0")
}

// An engaged squad holds its fight in place while its faction has at
// least two thirds of the enemy's strength in the cell, summed over every
// element there, so a stack of weak squads stays where one alone would
// leave.
func TestAnEngagedSquadRetreatsBelowTwoThirds(t *testing.T) {
	fight := loc("a", 2, 2)
	a := decide.Assessment{
		Own:      []decide.Element{engaged(squad("r1", fight))},
		Contacts: []decide.Contact{contact("b1", 300, fight), contact("b2", 300, fight)},
	}
	check(t, decide.Decide(open(5, 5), "red", a, nil), "r1 engage b1 a:2,2")

	a.Contacts[1].Strength = 301
	check(t, decide.Decide(open(5, 5), "red", a, nil), "r1 retreat b2 a:2,1")

	a.Own = []decide.Element{engaged(squad("r1", fight, 100)), engaged(squad("r2", fight, 100))}
	a.Contacts = []decide.Contact{contact("b1", 300, fight)}
	check(t, decide.Decide(open(5, 5), "red", a, nil), "r1 engage b1 a:2,2", "r2 engage b1 a:2,2")

	a.Own = a.Own[:1]
	check(t, decide.Decide(open(5, 5), "red", a, nil), "r1 retreat b1 a:2,1")
}

// An engaged element that holds its fight pursues when its faction at
// least matches the enemy's strength in the cell, and engages in place
// when it is outmatched.
func TestAnEngagedSquadPursuesAnEvenFight(t *testing.T) {
	fight := loc("a", 2, 2)
	a := decide.Assessment{
		Own:      []decide.Element{engaged(squad("r1", fight, 100, 100)), engaged(squad("r2", fight, 100, 100))},
		Contacts: []decide.Contact{contact("b1", 400, fight)},
	}
	check(t, decide.Decide(open(5, 5), "red", a, nil), "r1 pursue b1 a:2,2", "r2 pursue b1 a:2,2")

	a.Contacts[0].Strength = 401
	check(t, decide.Decide(open(5, 5), "red", a, nil), "r1 engage b1 a:2,2", "r2 engage b1 a:2,2")
}

// An element with no open neighbor free of the enemy holds its fight
// rather than retreat.
func TestARetreatWithNowhereToGoHolds(t *testing.T) {
	a := decide.Assessment{
		Own:      []decide.Element{engaged(scout("r1", loc("a", 0, 0)))},
		Contacts: []decide.Contact{contact("b1", 400, loc("a", 0, 0)), contact("b2", 100, loc("a", 1, 0))},
	}
	check(t, decide.Decide(open(2, 1), "red", a, nil), "r1 engage b1 a:0,0")
	check(t, decide.Decide(open(1, 1), "red", a, nil), "r1 engage b1 a:0,0")
}

// A ready or recovering squad within four steps of a fight its faction
// holds reinforces it, before any engage or objective, and any number may
// reinforce one fight. A squad out of reach and a scout do not, and the
// scout, with nothing else to do, rescouts the objective r4 secures; nor
// is a fight left by a retreat one to reinforce.
func TestSquadsReinforceAFight(t *testing.T) {
	fight := loc("a", 2, 2)
	recovering := squad("r3", loc("a", 5, 2))
	recovering.Status = decide.Recovering
	a := decide.Assessment{
		Own: []decide.Element{
			engaged(squad("r1", fight)),
			squad("r2", loc("a", 2, 5)),
			recovering,
			squad("r4", loc("a", 6, 6)),
			scout("r5", loc("a", 2, 3)),
		},
		Contacts: []decide.Contact{
			contact("b1", 300, fight),
			contact("b2", 100, loc("a", 3, 5)),
		},
		Objectives: []decide.Objective{unknown(loc("a", 6, 0))},
		Explored:   explored(open(7, 7)),
	}
	check(t, decide.Decide(open(7, 7), "red", a, nil),
		"r1 pursue b1 a:2,2", "r2 reinforce b1 a:2,2", "r3 reinforce b1 a:2,2",
		"r4 secure a:6,0", "r5 rescout a:6,0")

	a.Own[0] = engaged(scout("r1", fight))
	check(t, decide.Decide(open(7, 7), "red", a, nil),
		"r1 retreat b1 a:2,1", "r2 engage b2 a:3,5", "r3 engage b1 a:2,2",
		"r4 secure a:6,0", "r5 rescout a:6,0")
}

// A fight four steps off is in a squad's reach to reinforce, one five
// steps off is not, though a contact to engage is only in reach at three.
func TestReinforcesAFightWithinFourSteps(t *testing.T) {
	fight := loc("a", 0, 0)
	a := decide.Assessment{
		Own:      []decide.Element{engaged(squad("r1", fight)), squad("r2", loc("a", 4, 0))},
		Contacts: []decide.Contact{contact("b1", 300, fight)},
		Explored: explored(open(6, 1)),
	}
	check(t, decide.Decide(open(6, 1), "red", a, nil), "r1 pursue b1 a:0,0", "r2 reinforce b1 a:0,0")

	a.Own[1] = squad("r2", loc("a", 5, 0))
	check(t, decide.Decide(open(6, 1), "red", a, nil), "r1 pursue b1 a:0,0", "r2 hold")
}

// Distance is path distance: a contact one cell away across a wall is
// many steps off, and out of reach.
func TestAWallPutsANearContactOutOfReach(t *testing.T) {
	// Column x=1 is a wall but for its last cell.
	m := open(3, 6, decide.Point{X: 1, Y: 0}, decide.Point{X: 1, Y: 1}, decide.Point{X: 1, Y: 2},
		decide.Point{X: 1, Y: 3}, decide.Point{X: 1, Y: 4})
	a := decide.Assessment{
		Own:        []decide.Element{squad("r1", loc("a", 0, 0))},
		Contacts:   []decide.Contact{contact("b1", 100, loc("a", 2, 0))},
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
		Explored: explored(open(5, 5)),
	}
	check(t, decide.Decide(open(5, 5), "red", a, nil), "r1 secure a:0,1")

	a.Objectives = []decide.Objective{held("red", loc("a", 1, 0)), {At: loc("a", 2, 0), Holder: "red"}}
	check(t, decide.Decide(open(5, 5), "red", a, nil), "r1 secure a:2,0")
}

// No two elements secure one objective: each takes the nearest none
// before it in ID order took, and one with none left rescouts instead.
func TestSecuresEachObjectiveOnce(t *testing.T) {
	a := decide.Assessment{
		Own: []decide.Element{
			scout("r3", loc("a", 4, 4)),
			scout("r1", loc("a", 0, 0)),
			scout("r2", loc("a", 1, 0)),
		},
		Objectives: []decide.Objective{unknown(loc("a", 2, 0)), unknown(loc("a", 4, 0))},
		Explored:   explored(open(5, 5)),
	}
	check(t, decide.Decide(open(5, 5), "red", a, nil),
		"r1 secure a:2,0", "r2 secure a:4,0", "r3 rescout a:4,0")
}

// An element keeps the objective it was securing, though another is now
// nearer to it, and its standing target is claimed before any element
// picks: here r1 would otherwise take r2's.
func TestKeepsAStandingTarget(t *testing.T) {
	a := decide.Assessment{
		Own:        []decide.Element{scout("r1", loc("a", 3, 0)), scout("r2", loc("a", 0, 4))},
		Objectives: []decide.Objective{unknown(loc("a", 4, 0)), unknown(loc("a", 0, 0))},
		Explored:   explored(open(5, 5)),
	}
	far, near := loc("a", 0, 0), loc("a", 4, 0)
	standing := []decide.Decision{
		{Element: "r1", Rule: decide.Secure, Target: &far},
		{Element: "r2", Rule: decide.Secure, Target: &near},
	}
	check(t, decide.Decide(open(5, 5), "red", a, standing), "r1 secure a:0,0", "r2 secure a:4,0")

	// Once r1's objective is held, it picks again, and with none left to
	// secure, rescouts the one r2 secures.
	a.Objectives[1] = held("red", loc("a", 0, 0))
	check(t, decide.Decide(open(5, 5), "red", a, standing), "r1 rescout a:4,0", "r2 secure a:4,0")
}

// An engage outranks a standing target, and a standing target the element
// can no longer reach is dropped.
func TestAStandingTargetYields(t *testing.T) {
	target := loc("a", 4, 4)
	standing := []decide.Decision{{Element: "r1", Rule: decide.Secure, Target: &target}}
	a := decide.Assessment{
		Own:        []decide.Element{squad("r1", loc("a", 0, 0))},
		Contacts:   []decide.Contact{contact("b1", 100, loc("a", 0, 1))},
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
		Own:        []decide.Element{squad("r1", loc("a", 0, 0))},
		Contacts:   []decide.Contact{contact("b1", 100, loc("b", 1, 0))},
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
		Explored:   explored(m),
	}
	check(t, decide.Decide(m, "red", a, nil), "r1 hold")
}

// An objective the assessment does not list is undiscovered, and never
// secured, though the map places one there; with the board explored and
// no objective discovered, an element holds.
func TestSecuresOnlyADiscoveredObjective(t *testing.T) {
	m := open(5, 5)
	m.Sectors[0].Objectives = []decide.Point{{X: 4, Y: 4}}
	a := decide.Assessment{Own: []decide.Element{squad("r1", loc("a", 0, 0))}, Explored: explored(m)}
	check(t, decide.Decide(m, "red", a, nil), "r1 hold")
}

// With no objective to secure, an element searches the nearest unexplored
// cell, by path distance: a cell one column off across a wall is many
// steps away. A cell it stands in counts as explored.
func TestSearchesTheNearestUnexploredCell(t *testing.T) {
	// Column x=1 is a wall but for its last cell.
	m := open(3, 6, decide.Point{X: 1, Y: 0}, decide.Point{X: 1, Y: 1}, decide.Point{X: 1, Y: 2},
		decide.Point{X: 1, Y: 3}, decide.Point{X: 1, Y: 4})
	a := decide.Assessment{
		Own:      []decide.Element{squad("r1", loc("a", 0, 0))},
		Explored: explored(m, loc("a", 0, 0), loc("a", 2, 0), loc("a", 0, 4)),
	}
	check(t, decide.Decide(m, "red", a, nil), "r1 search a:0,4")
}

// Searchers spread: each takes the nearest unexplored cell farther than
// two cells from every cell already taken, and only when none is does it
// take the nearest free one.
func TestSearchersSpread(t *testing.T) {
	a := decide.Assessment{Own: []decide.Element{scout("r1", loc("a", 0, 0)), scout("r2", loc("a", 1, 0))}}
	check(t, decide.Decide(open(7, 1), "red", a, nil), "r1 search a:2,0", "r2 search a:5,0")
	check(t, decide.Decide(open(4, 1), "red", a, nil), "r1 search a:2,0", "r2 search a:3,0")

	// With one unexplored cell, the second searcher holds.
	check(t, decide.Decide(open(3, 1), "red", a, nil), "r1 search a:2,0", "r2 hold")
}

// Scouts search before squads, so the nearest cell goes to a scout though
// a squad comes first by ID.
func TestScoutsSearchFirst(t *testing.T) {
	a := decide.Assessment{Own: []decide.Element{squad("r1", loc("a", 0, 0)), scout("r2", loc("a", 0, 0))}}
	check(t, decide.Decide(open(7, 1), "red", a, nil), "r1 search a:4,0", "r2 search a:1,0")
}

// A scout searches while an unexplored cell is in its reach, though an
// objective is there to secure; a squad secures it. Once the board is
// explored, the scout secures too.
func TestAScoutSearchesBeforeItSecures(t *testing.T) {
	m := open(5, 1)
	a := decide.Assessment{
		Own:        []decide.Element{scout("r1", loc("a", 0, 0))},
		Objectives: []decide.Objective{unknown(loc("a", 2, 0))},
		Explored:   explored(m, loc("a", 4, 0)),
	}
	check(t, decide.Decide(m, "red", a, nil), "r1 search a:4,0")

	a.Own = []decide.Element{squad("r1", loc("a", 0, 0))}
	check(t, decide.Decide(m, "red", a, nil), "r1 secure a:2,0")

	a.Own = []decide.Element{scout("r1", loc("a", 0, 0))}
	a.Explored = explored(m)
	check(t, decide.Decide(m, "red", a, nil), "r1 secure a:2,0")
}

// An element keeps the cell it was searching while that cell is still
// unexplored, though another is now nearer, and picks again once it is
// explored.
func TestKeepsAStandingSearch(t *testing.T) {
	m := open(5, 1)
	far := loc("a", 4, 0)
	standing := []decide.Decision{{Element: "r1", Rule: decide.Search, Target: &far}}
	a := decide.Assessment{Own: []decide.Element{scout("r1", loc("a", 2, 0))}, Explored: []decide.Location{loc("a", 3, 0)}}
	check(t, decide.Decide(m, "red", a, standing), "r1 search a:4,0")

	a.Explored = append(a.Explored, far)
	check(t, decide.Decide(m, "red", a, standing), "r1 search a:1,0")
}

// stale returns o, last seen age rounds ago.
func stale(o decide.Objective, age int) decide.Objective {
	o.Age = age
	return o
}

// An element with nothing else to do rescouts the objective not held that
// was seen longest ago, though a nearer one is fresher, ties going to the
// nearest, and though another element secures it. One its faction knows
// it holds is not rescouted, however stale, nor one the element stands on.
func TestRescoutsTheStalestObjectiveNotHeld(t *testing.T) {
	m := open(9, 1)
	a := decide.Assessment{
		Own: []decide.Element{
			squad("r1", loc("a", 4, 0)),
			squad("r2", loc("a", 1, 0)),
			squad("r3", loc("a", 7, 0)),
			squad("r4", loc("a", 3, 0)),
		},
		Objectives: []decide.Objective{
			stale(held("blue", loc("a", 5, 0)), 1),
			stale(held("blue", loc("a", 0, 0)), 3),
			stale(held("red", loc("a", 2, 0)), 6),
			stale(unknown(loc("a", 8, 0)), 3),
		},
		Explored: explored(m),
	}
	check(t, decide.Decide(m, "red", a, nil),
		"r1 secure a:5,0", "r2 secure a:0,0", "r3 secure a:8,0", "r4 rescout a:0,0")

	a.Own[3] = squad("r4", loc("a", 0, 0))
	check(t, decide.Decide(m, "red", a, nil),
		"r1 secure a:5,0", "r2 secure a:0,0", "r3 secure a:8,0", "r4 rescout a:8,0")
}

// No two elements rescout one objective: each takes the stalest none
// before it in ID order took, and an unreachable one is skipped.
func TestRescoutsEachObjectiveOnce(t *testing.T) {
	m := open(5, 3, decide.Point{X: 3, Y: 2}, decide.Point{X: 4, Y: 1})
	a := decide.Assessment{
		Own: []decide.Element{
			squad("r1", loc("a", 2, 0)),
			squad("r2", loc("a", 0, 2)),
			squad("r3", loc("a", 0, 1)),
			scout("r4", loc("a", 1, 1)),
		},
		Objectives: []decide.Objective{
			stale(held("blue", loc("a", 3, 0)), 1),
			stale(unknown(loc("a", 0, 0)), 2),
			stale(held("blue", loc("a", 4, 2)), 9), // walled off
		},
		Explored: explored(m),
	}
	check(t, decide.Decide(m, "red", a, nil),
		"r1 secure a:3,0", "r2 secure a:0,0", "r3 rescout a:0,0", "r4 rescout a:3,0")
}

// With every objective known to be its faction's and nothing unexplored,
// an element holds.
func TestHoldsWhenEveryObjectiveIsHeld(t *testing.T) {
	m := open(5, 5)
	a := decide.Assessment{
		Own:        []decide.Element{squad("r1", loc("a", 0, 0)), scout("r2", loc("a", 4, 4))},
		Objectives: []decide.Objective{stale(held("red", loc("a", 2, 2)), 8), held("red", loc("a", 4, 0))},
		Explored:   explored(m),
	}
	check(t, decide.Decide(m, "red", a, nil), "r1 hold", "r2 hold")
}

// An element keeps the objective it was rescouting while it is still not
// held, though another is now staler, and its standing target is claimed
// before any element picks; once the objective is held, it picks again.
func TestKeepsAStandingRescout(t *testing.T) {
	m := open(5, 1)
	a := decide.Assessment{
		Own: []decide.Element{squad("r1", loc("a", 2, 0)), squad("r2", loc("a", 3, 0)), squad("r3", loc("a", 1, 0))},
		Objectives: []decide.Objective{
			stale(held("blue", loc("a", 0, 0)), 5),
			stale(held("blue", loc("a", 4, 0)), 1),
		},
		Explored: explored(m),
	}
	far, near := loc("a", 4, 0), loc("a", 0, 0)
	standing := []decide.Decision{
		{Element: "r1", Rule: decide.Rescout, Target: &far},
		{Element: "r2", Rule: decide.Secure, Target: &near},
		{Element: "r3", Rule: decide.Secure, Target: &far},
	}
	check(t, decide.Decide(m, "red", a, standing), "r1 rescout a:4,0", "r2 secure a:0,0", "r3 secure a:4,0")

	a.Objectives[1] = held("red", far)
	check(t, decide.Decide(m, "red", a, standing), "r1 rescout a:0,0", "r2 secure a:0,0", "r3 hold")
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
