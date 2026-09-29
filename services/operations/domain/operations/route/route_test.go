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

// A force takes one step and a scout two; an element with no target, on its
// target, or with an unreachable target holds and gets no order.
func TestPlanMovesByKind(t *testing.T) {
	elements := []route.Element{
		{ID: "f", Kind: "force", At: loc("a", 2, 0)},
		{ID: "s", Kind: "scout", At: loc("a", 0, 2)},
		{ID: "idle", Kind: "force", At: loc("a", 0, 0)},
		{ID: "there", Kind: "force", At: loc("b", 1, 0)},
		{ID: "stuck", Kind: "force", At: loc("a", 1, 0)},
	}
	targets := map[string]route.Location{
		"f":     loc("b", 2, 0),
		"s":     loc("b", 2, 0),
		"there": loc("b", 1, 0),
		"stuck": loc("a", 1, 1),
	}
	got := route.Plan(twoSectors, elements, targets)
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
		{ID: "c", Kind: "force", At: loc("l", 4, 0)},
		{ID: "d", Kind: "force", At: loc("l", 3, 0)},
		{ID: "e", Kind: "force", At: loc("l", 5, 0)},
	}
	target := loc("l", 2, 0)
	targets := map[string]route.Location{"a": target, "b": target, "c": target, "d": loc("l", 4, 0), "e": loc("l", 4, 0)}
	got := route.Plan(line, elements, targets)
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

	// A scout two cells behind a force that stays stops one short of it; a
	// force following the scout steps into the cell the scout leaves.
	got = route.Plan(line, []route.Element{
		{ID: "f", Kind: "force", At: loc("l", 0, 0)},
		{ID: "s", Kind: "scout", At: loc("l", 1, 0)},
		{ID: "w", Kind: "force", At: loc("l", 3, 0)},
	}, map[string]route.Location{"f": loc("l", 5, 0), "s": loc("l", 5, 0)})
	want = []route.Order{
		{Element: "f", Steps: []route.Location{loc("l", 1, 0)}},
		{Element: "s", Steps: []route.Location{loc("l", 2, 0)}},
	}
	if !equal(got, want) {
		t.Errorf("Plan = %v, want %v", got, want)
	}
}

func equal(a, b []route.Order) bool {
	return slices.EqualFunc(a, b, func(x, y route.Order) bool {
		return x.Element == y.Element && slices.Equal(x.Steps, y.Steps)
	})
}
