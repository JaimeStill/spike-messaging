package route_test

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/JaimeStill/spike-messaging/services/operations/domain/operations/route"
)

func loc(sector string, x, y int) route.Location {
	return route.Location{Sector: sector, Point: route.Point{X: x, Y: y}}
}

// twoSectors is a 3×3 sector a with an obstacle at 1,1 and a gate at 2,2,
// linked to the gate at 0,0 of a 3×1 sector b, whose objective is at 2,0.
//
//	a:  . . .     b:  G . O
//	    . # .
//	    . . G
var twoSectors = route.Map{Sectors: []route.Sector{
	{
		ID: "a", Width: 3, Height: 3,
		Obstacles: []route.Point{{X: 1, Y: 1}},
		Gates:     []route.Gate{{At: route.Point{X: 2, Y: 2}, To: loc("b", 0, 0)}},
	},
	{
		ID: "b", Width: 3, Height: 1,
		Objectives: []route.Point{{X: 2, Y: 0}},
		Gates:      []route.Gate{{At: route.Point{X: 0, Y: 0}, To: loc("a", 2, 2)}},
	},
}}

// The map decodes from the exercise.started event's JSON, a location's
// point flattened beside its sector.
func TestMapDecodesTheExerciseShape(t *testing.T) {
	var m route.Map
	data := `{"sectors":[{"id":"a","width":2,"height":1,"obstacles":[],"objectives":[{"x":1,"y":0}],
		"gates":[{"at":{"x":0,"y":0},"to":{"sector":"b","x":3,"y":4}}]}]}`
	if err := json.Unmarshal([]byte(data), &m); err != nil {
		t.Fatal(err)
	}
	if got := m.Sectors[0].Gates[0].To; got != loc("b", 3, 4) {
		t.Errorf("gate links to %v", got)
	}
	if got := m.Objectives(); !slices.Equal(got, []route.Location{loc("a", 1, 0)}) {
		t.Errorf("Objectives = %v", got)
	}
}

func TestPath(t *testing.T) {
	for name, tc := range map[string]struct {
		from, to route.Location
		want     []route.Location
		ok       bool
	}{
		"here": {loc("a", 0, 0), loc("a", 0, 0), nil, true},
		// Around the obstacle, preferring right over down at 0,0.
		"around the obstacle": {loc("a", 0, 1), loc("a", 2, 1), []route.Location{
			loc("a", 0, 0), loc("a", 1, 0), loc("a", 2, 0), loc("a", 2, 1),
		}, true},
		// Entering the gate is one step and traversing it another.
		"through the gate": {loc("a", 2, 1), loc("b", 2, 0), []route.Location{
			loc("a", 2, 2), loc("b", 0, 0), loc("b", 1, 0), loc("b", 2, 0),
		}, true},
		"into an obstacle": {loc("a", 0, 0), loc("a", 1, 1), nil, false},
		"off the map":      {loc("a", 0, 0), loc("c", 0, 0), nil, false},
	} {
		got, ok := twoSectors.Path(tc.from, tc.to)
		if ok != tc.ok || !slices.Equal(got, tc.want) {
			t.Errorf("%s: Path = %v, %v; want %v, %v", name, got, ok, tc.want, tc.ok)
		}
	}
}

// A squad takes one step and a scout two; an element with no target, on its
// target, or with an unreachable target holds and gets no order.
func TestPlanMovesByKind(t *testing.T) {
	elements := []route.Element{
		{ID: "f", Kind: "squad", At: loc("a", 2, 0)},
		{ID: "s", Kind: "scout", At: loc("a", 0, 2)},
		{ID: "idle", Kind: "squad", At: loc("a", 0, 0)},
		{ID: "there", Kind: "squad", At: loc("b", 1, 0)},
		{ID: "stuck", Kind: "squad", At: loc("a", 1, 0)},
	}
	targets := map[string]route.Location{
		"f":     loc("b", 2, 0),
		"s":     loc("b", 2, 0),
		"there": loc("b", 1, 0),
		"stuck": loc("a", 1, 1),
	}
	got := route.Plan(twoSectors, elements, targets, nil)
	want := []route.Order{
		{Element: "f", Steps: []route.Location{loc("a", 2, 1)}},
		{Element: "s", Steps: []route.Location{loc("a", 1, 2), loc("a", 2, 2)}},
	}
	if !equal(got, want) {
		t.Errorf("Plan = %v, want %v", got, want)
	}
}

// No two elements of the faction end on one cell, by exercise's rule: an
// element may follow another into the cell it leaves, and two may swap;
// of movers that would share a cell the lowest ID keeps it, and a mover
// stops short of a cell an element that stays holds.
func TestPlanKeepsElementsApart(t *testing.T) {
	line := route.Map{Sectors: []route.Sector{{ID: "l", Width: 6, Height: 1}}}
	elements := []route.Element{
		{ID: "a", Kind: "scout", At: loc("l", 0, 0)},
		{ID: "b", Kind: "scout", At: loc("l", 1, 0)},
		{ID: "c", Kind: "squad", At: loc("l", 4, 0)},
		{ID: "d", Kind: "squad", At: loc("l", 3, 0)},
		{ID: "e", Kind: "squad", At: loc("l", 5, 0)},
	}
	target := loc("l", 2, 0)
	targets := map[string]route.Location{"a": target, "b": target, "c": target, "d": loc("l", 4, 0), "e": loc("l", 4, 0)}
	got := route.Plan(line, elements, targets, nil)
	// a and b both reach 2,0 and a keeps it, so b holds; c and d swap 3,0
	// and 4,0; e's step onto 4,0 is where d ends, so e holds.
	want := []route.Order{
		{Element: "a", Steps: []route.Location{loc("l", 1, 0), loc("l", 2, 0)}},
		{Element: "c", Steps: []route.Location{loc("l", 3, 0)}},
		{Element: "d", Steps: []route.Location{loc("l", 4, 0)}},
	}
	if !equal(got, want) {
		t.Errorf("Plan = %v, want %v", got, want)
	}

	// A scout two cells behind a squad that stays stops one short of it; a
	// squad following the scout steps into the cell the scout leaves.
	got = route.Plan(line, []route.Element{
		{ID: "f", Kind: "squad", At: loc("l", 0, 0)},
		{ID: "s", Kind: "scout", At: loc("l", 1, 0)},
		{ID: "w", Kind: "squad", At: loc("l", 3, 0)},
	}, map[string]route.Location{"f": loc("l", 5, 0), "s": loc("l", 5, 0)}, nil)
	want = []route.Order{
		{Element: "f", Steps: []route.Location{loc("l", 1, 0)}},
		{Element: "s", Steps: []route.Location{loc("l", 2, 0)}},
	}
	if !equal(got, want) {
		t.Errorf("Plan = %v, want %v", got, want)
	}
}

// An engaged element stays in its fight, whatever its target, unless its
// rule is retreat and its target is one step away: then its order is that
// step, a retreat, even for a scout. A retreat to a cell farther away, and
// any order for a recovering element, gives no order.
func TestPlanHoldsEngagedAndRecoveringElements(t *testing.T) {
	elements := []route.Element{
		{ID: "e", Kind: "squad", Status: route.StatusEngaged, At: loc("a", 0, 0)},
		{ID: "far", Kind: "scout", Status: route.StatusEngaged, At: loc("a", 0, 2)},
		{ID: "r", Kind: "scout", Status: route.StatusEngaged, At: loc("a", 2, 0)},
		{ID: "rec", Kind: "scout", Status: route.StatusRecovering, At: loc("b", 1, 0)},
	}
	targets := map[string]route.Location{
		"e":   loc("a", 0, 2),
		"far": loc("a", 2, 1),
		"r":   loc("a", 2, 1),
		"rec": loc("b", 2, 0),
	}
	rules := map[string]string{"e": "secure", "far": route.RuleRetreat, "r": route.RuleRetreat, "rec": "secure"}
	got := route.Plan(twoSectors, elements, targets, rules)
	want := []route.Order{{Element: "r", Steps: []route.Location{loc("a", 2, 1)}, Retreat: true}}
	if !equal(got, want) {
		t.Errorf("Plan = %v, want %v", got, want)
	}
}

// Reinforcements may end in a cell where one of the faction's engaged
// elements stands, as exercise lets them join a fight; elsewhere movers are
// still kept apart.
func TestPlanLetsReinforcementsJoinAFight(t *testing.T) {
	line := route.Map{Sectors: []route.Sector{{ID: "l", Width: 7, Height: 1}}}
	fight := loc("l", 2, 0)
	elements := []route.Element{
		{ID: "a", Kind: "squad", Status: route.StatusReady, At: loc("l", 1, 0)},
		{ID: "b", Kind: "scout", Status: route.StatusReady, At: loc("l", 0, 0)},
		{ID: "e", Kind: "squad", Status: route.StatusEngaged, At: fight},
		{ID: "x", Kind: "squad", Status: route.StatusReady, At: loc("l", 4, 0)},
		{ID: "y", Kind: "squad", Status: route.StatusReady, At: loc("l", 6, 0)},
	}
	targets := map[string]route.Location{"a": fight, "b": fight, "e": fight, "x": loc("l", 5, 0), "y": loc("l", 5, 0)}
	rules := map[string]string{"a": "reinforce", "b": "reinforce", "e": "engage", "x": "secure", "y": "secure"}
	got := route.Plan(line, elements, targets, rules)
	// a and b both join e in its fight; x and y would share 5,0, and x keeps
	// it.
	want := []route.Order{
		{Element: "a", Steps: []route.Location{fight}},
		{Element: "b", Steps: []route.Location{loc("l", 1, 0), fight}},
		{Element: "x", Steps: []route.Location{loc("l", 5, 0)}},
	}
	if !equal(got, want) {
		t.Errorf("Plan = %v, want %v", got, want)
	}
}

// An engaged element whose rule is pursue gets an order with no steps,
// flagged as a pursuit; one that engages in place gets none.
func TestPlanPursuesInPlace(t *testing.T) {
	elements := []route.Element{
		{ID: "e", Kind: "squad", Status: route.StatusEngaged, At: loc("a", 0, 0)},
		{ID: "p", Kind: "scout", Status: route.StatusEngaged, At: loc("a", 2, 0)},
	}
	targets := map[string]route.Location{"e": loc("a", 0, 0), "p": loc("a", 2, 0)}
	rules := map[string]string{"e": "engage", "p": route.RulePursue}
	got := route.Plan(twoSectors, elements, targets, rules)
	want := []route.Order{{Element: "p", Steps: []route.Location{}, Pursue: true}}
	if !equal(got, want) || got[0].Steps == nil {
		t.Errorf("Plan = %v, want %v", got, want)
	}
}

// An order's retreat and pursue flags are left out of its JSON unless set.
func TestOrderEncodesFlagsOnlyWhenSet(t *testing.T) {
	for o, want := range map[*route.Order]string{
		{Element: "r", Steps: []route.Location{}}:                `{"element":"r","steps":[]}`,
		{Element: "r", Steps: []route.Location{}, Retreat: true}: `{"element":"r","steps":[],"retreat":true}`,
		{Element: "r", Steps: []route.Location{}, Pursue: true}:  `{"element":"r","steps":[],"pursue":true}`,
	} {
		b, err := json.Marshal(o)
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != want {
			t.Errorf("Marshal = %s, want %s", b, want)
		}
	}
}

func equal(a, b []route.Order) bool {
	return slices.EqualFunc(a, b, func(x, y route.Order) bool {
		return x.Element == y.Element && x.Retreat == y.Retreat && x.Pursue == y.Pursue && slices.Equal(x.Steps, y.Steps)
	})
}
