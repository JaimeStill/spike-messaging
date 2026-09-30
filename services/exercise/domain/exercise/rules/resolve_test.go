package rules_test

import (
	"reflect"
	"slices"
	"testing"

	"github.com/JaimeStill/spike-messaging/services/exercise/domain/exercise/rules"
)

func TestMove(t *testing.T) {
	tests := []struct {
		name   string
		start  []rules.Element
		orders []rules.Order
		want   map[string]rules.Location
	}{
		{
			name:   "a force steps one cell",
			start:  []rules.Element{force("r1", "red", 2, loc("a", 0, 0))},
			orders: []rules.Order{order("r1", loc("a", 1, 0))},
			want:   map[string]rules.Location{"r1": loc("a", 1, 0)},
		},
		{
			name:   "a scout steps two cells",
			start:  []rules.Element{scout("r1", "red", loc("a", 0, 0))},
			orders: []rules.Order{order("r1", loc("a", 0, 1), loc("a", 1, 1))},
			want:   map[string]rules.Location{"r1": loc("a", 1, 1)},
		},
		{
			name:   "an order with no steps holds the element",
			start:  []rules.Element{force("r1", "red", 2, loc("a", 0, 0))},
			orders: []rules.Order{order("r1")},
			want:   map[string]rules.Location{"r1": loc("a", 0, 0)},
		},
		{
			name:   "a force ordered two steps stays",
			start:  []rules.Element{force("r1", "red", 2, loc("a", 0, 0))},
			orders: []rules.Order{order("r1", loc("a", 1, 0), loc("a", 2, 0))},
			want:   map[string]rules.Location{"r1": loc("a", 0, 0)},
		},
		{
			name:   "a scout ordered three steps stays",
			start:  []rules.Element{scout("r1", "red", loc("a", 0, 0))},
			orders: []rules.Order{order("r1", loc("a", 1, 0), loc("a", 2, 0), loc("a", 3, 0))},
			want:   map[string]rules.Location{"r1": loc("a", 0, 0)},
		},
		{
			name:   "a diagonal step is refused",
			start:  []rules.Element{force("r1", "red", 2, loc("a", 0, 0))},
			orders: []rules.Order{order("r1", loc("a", 1, 1))},
			want:   map[string]rules.Location{"r1": loc("a", 0, 0)},
		},
		{
			name:   "a jump of two cells in one step is refused",
			start:  []rules.Element{scout("r1", "red", loc("a", 0, 0))},
			orders: []rules.Order{order("r1", loc("a", 2, 0))},
			want:   map[string]rules.Location{"r1": loc("a", 0, 0)},
		},
		{
			name:   "a step off the grid is refused",
			start:  []rules.Element{force("r1", "red", 2, loc("a", 0, 0))},
			orders: []rules.Order{order("r1", loc("a", -1, 0))},
			want:   map[string]rules.Location{"r1": loc("a", 0, 0)},
		},
		{
			name:   "a step into an obstacle is refused",
			start:  []rules.Element{force("r1", "red", 2, loc("a", 2, 1))},
			orders: []rules.Order{order("r1", loc("a", 2, 2))},
			want:   map[string]rules.Location{"r1": loc("a", 2, 1)},
		},
		{
			name:   "a scout whose second step is illegal refuses the whole order",
			start:  []rules.Element{scout("r1", "red", loc("a", 1, 1))},
			orders: []rules.Order{order("r1", loc("a", 2, 1), loc("a", 2, 2))},
			want:   map[string]rules.Location{"r1": loc("a", 1, 1)},
		},
		{
			name:   "entering a gate does not traverse it",
			start:  []rules.Element{force("r1", "red", 2, loc("a", 5, 4))},
			orders: []rules.Order{order("r1", loc("a", 5, 5))},
			want:   map[string]rules.Location{"r1": loc("a", 5, 5)},
		},
		{
			name:   "a step from a gate to its link traverses it",
			start:  []rules.Element{force("r1", "red", 2, loc("a", 5, 5))},
			orders: []rules.Order{order("r1", loc("b", 0, 0))},
			want:   map[string]rules.Location{"r1": loc("b", 0, 0)},
		},
		{
			name:   "a scout enters a gate and traverses it in one round",
			start:  []rules.Element{scout("r1", "red", loc("a", 4, 5))},
			orders: []rules.Order{order("r1", loc("a", 5, 5), loc("b", 0, 0))},
			want:   map[string]rules.Location{"r1": loc("b", 0, 0)},
		},
		{
			name:   "a gate still allows an ordinary step",
			start:  []rules.Element{force("r1", "red", 2, loc("a", 5, 5))},
			orders: []rules.Order{order("r1", loc("a", 4, 5))},
			want:   map[string]rules.Location{"r1": loc("a", 4, 5)},
		},
		{
			name:   "a step into another sector away from a gate is refused",
			start:  []rules.Element{force("r1", "red", 2, loc("a", 0, 0))},
			orders: []rules.Order{order("r1", loc("b", 0, 0))},
			want:   map[string]rules.Location{"r1": loc("a", 0, 0)},
		},
		{
			name:   "a step from a gate to a cell other than its link is refused",
			start:  []rules.Element{force("r1", "red", 2, loc("a", 5, 5))},
			orders: []rules.Order{order("r1", loc("b", 1, 0))},
			want:   map[string]rules.Location{"r1": loc("a", 5, 5)},
		},
		{
			name:   "an order for an unknown element is ignored",
			start:  []rules.Element{force("r1", "red", 2, loc("a", 0, 0))},
			orders: []rules.Order{order("zz", loc("a", 1, 0))},
			want:   map[string]rules.Location{"r1": loc("a", 0, 0)},
		},
		{
			name:  "the last order for an element wins",
			start: []rules.Element{force("r1", "red", 2, loc("a", 0, 0))},
			orders: []rules.Order{
				order("r1", loc("a", 1, 0)),
				order("r1", loc("a", 0, 1)),
			},
			want: map[string]rules.Location{"r1": loc("a", 0, 1)},
		},
		{
			name: "two elements of one faction ending on one cell are both refused",
			start: []rules.Element{
				force("r1", "red", 2, loc("a", 0, 1)),
				force("r2", "red", 2, loc("a", 1, 0)),
			},
			orders: []rules.Order{order("r1", loc("a", 1, 1)), order("r2", loc("a", 1, 1))},
			want:   map[string]rules.Location{"r1": loc("a", 0, 1), "r2": loc("a", 1, 0)},
		},
		{
			name: "moving onto a faction member that stays is refused",
			start: []rules.Element{
				force("r1", "red", 2, loc("a", 0, 0)),
				force("r2", "red", 2, loc("a", 1, 0)),
			},
			orders: []rules.Order{order("r1", loc("a", 1, 0))},
			want:   map[string]rules.Location{"r1": loc("a", 0, 0), "r2": loc("a", 1, 0)},
		},
		{
			name: "moving into a cell a faction member leaves is allowed",
			start: []rules.Element{
				force("r1", "red", 2, loc("a", 0, 0)),
				force("r2", "red", 2, loc("a", 1, 0)),
			},
			orders: []rules.Order{order("r1", loc("a", 1, 0)), order("r2", loc("a", 1, 1))},
			want:   map[string]rules.Location{"r1": loc("a", 1, 0), "r2": loc("a", 1, 1)},
		},
		{
			name: "a refusal cascades to the element that would have followed",
			start: []rules.Element{
				force("r1", "red", 2, loc("a", 0, 0)),
				force("r2", "red", 2, loc("a", 1, 0)),
				force("r3", "red", 2, loc("a", 1, 2)),
				force("r4", "red", 2, loc("a", 2, 1)),
			},
			// r3 and r4 collide on 1,1 and stay; r2 then cannot leave for
			// 1,1 either, so r1 cannot enter r2's cell.
			orders: []rules.Order{
				order("r1", loc("a", 1, 0)),
				order("r2", loc("a", 1, 1)),
				order("r3", loc("a", 1, 1)),
				order("r4", loc("a", 1, 1)),
			},
			want: map[string]rules.Location{
				"r1": loc("a", 0, 0), "r2": loc("a", 1, 0),
				"r3": loc("a", 1, 2), "r4": loc("a", 2, 1),
			},
		},
		{
			name: "elements of one faction may swap cells",
			start: []rules.Element{
				force("r1", "red", 2, loc("a", 0, 0)),
				force("r2", "red", 2, loc("a", 1, 0)),
			},
			orders: []rules.Order{order("r1", loc("a", 1, 0)), order("r2", loc("a", 0, 0))},
			want:   map[string]rules.Location{"r1": loc("a", 1, 0), "r2": loc("a", 0, 0)},
		},
		{
			name: "enemies swapping cells do not meet",
			start: []rules.Element{
				force("r1", "red", 2, loc("a", 0, 0)),
				force("b1", "blue", 3, loc("a", 1, 0)),
			},
			orders: []rules.Order{order("r1", loc("a", 1, 0)), order("b1", loc("a", 0, 0))},
			want:   map[string]rules.Location{"r1": loc("a", 1, 0), "b1": loc("a", 0, 0)},
		},
		{
			name: "a scout passing through an enemy's cell does not meet it",
			start: []rules.Element{
				scout("r1", "red", loc("a", 0, 0)),
				force("b1", "blue", 3, loc("a", 1, 0)),
			},
			orders: []rules.Order{order("r1", loc("a", 1, 0), loc("a", 2, 0))},
			want:   map[string]rules.Location{"r1": loc("a", 2, 0), "b1": loc("a", 1, 0)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := state(tt.start...)
			if err := s.Validate(); err != nil {
				t.Fatalf("start: %v", err)
			}
			next, _, _, _ := rules.Resolve(s, 1, 10, tt.orders)
			if len(next.Elements) != len(tt.want) {
				t.Fatalf("elements = %v, want %d", ids(next.Elements), len(tt.want))
			}
			for id, want := range tt.want {
				e, ok := find(next, id)
				if !ok {
					t.Errorf("%s: missing", id)
					continue
				}
				if e.At != want {
					t.Errorf("%s at %s, want %s", id, e.At.Key(), want.Key())
				}
			}
			if err := next.Validate(); err != nil {
				t.Errorf("next: %v", err)
			}
		})
	}
}

// Engagement is attrition: each faction loses half the other's total in
// the cell, rounded up, both at once, weakest element first.
func TestEngage(t *testing.T) {
	c := loc("a", 1, 1)
	tests := []struct {
		name  string
		start []rules.Element
		want  map[string]int // survivor ID -> strength
	}{
		{
			name:  "each side loses half the other's strength, rounded up",
			start: []rules.Element{force("r1", "red", 5, c), force("b1", "blue", 3, c)},
			want:  map[string]int{"r1": 3},
		},
		{
			name: "the loss falls on the weakest element first",
			start: []rules.Element{
				force("r1", "red", 4, c), force("r2", "red", 2, c),
				force("b1", "blue", 1, c),
			},
			want: map[string]int{"r1": 4, "r2": 1},
		},
		{
			name: "the loss spreads to the next weakest once one falls, and both sides survive",
			start: []rules.Element{
				force("r1", "red", 4, c), scout("r2", "red", c),
				force("b1", "blue", 5, c),
			},
			want: map[string]int{"r1": 2, "b1": 2},
		},
		{
			name: "equal strengths lose in ID order",
			start: []rules.Element{
				force("r2", "red", 2, c), force("r1", "red", 2, c),
				force("b1", "blue", 1, c),
			},
			want: map[string]int{"r1": 1, "r2": 2},
		},
		{
			name: "three or more elements on a side",
			start: []rules.Element{
				force("b1", "blue", 5, c), scout("b2", "blue", c), force("b3", "blue", 2, c),
				force("r1", "red", 3, c), scout("r2", "red", c),
			},
			want: map[string]int{"b1": 5, "b3": 1},
		},
		{
			name:  "equal sides can destroy each other",
			start: []rules.Element{scout("r1", "red", c), scout("b1", "blue", c)},
			want:  map[string]int{},
		},
		{
			name: "only cells holding both factions engage",
			start: []rules.Element{
				force("r1", "red", 2, loc("a", 0, 0)),
				force("b1", "blue", 3, loc("a", 1, 0)),
			},
			want: map[string]int{"r1": 2, "b1": 3},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			next, _, _, _ := rules.Resolve(state(tt.start...), 1, 10, nil)
			got := map[string]int{}
			for _, e := range next.Elements {
				got[e.ID] = e.Strength
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("survivors = %v, want %v", got, tt.want)
			}
			if !slices.IsSorted(ids(next.Elements)) {
				t.Errorf("elements %v are not sorted by ID", ids(next.Elements))
			}
		})
	}
}

// A fight lasts while both sides stay in the cell: a force of 4 against one
// of 3 takes two rounds, and the resolution records each round's strengths
// before and after, and the loss in the round it happens.
func TestAFightLastsRounds(t *testing.T) {
	c := loc("a", 1, 1)
	s := state(force("r1", "red", 4, c), force("b1", "blue", 3, c))
	want := []rules.Resolution{
		{
			Engagements: []rules.Engagement{{At: c, Elements: []rules.Engaged{
				{ID: "b1", Faction: "blue", Before: 3, After: 1},
				{ID: "r1", Faction: "red", Before: 4, After: 2},
			}}},
			Captures: []rules.Capture{},
			Losses:   []rules.Loss{},
		},
		{
			Engagements: []rules.Engagement{{At: c, Elements: []rules.Engaged{
				{ID: "b1", Faction: "blue", Before: 1, After: 0},
				{ID: "r1", Faction: "red", Before: 2, After: 1},
			}}},
			Captures: []rules.Capture{},
			Losses:   []rules.Loss{{ID: "b1", Faction: "blue"}},
		},
		{Engagements: []rules.Engagement{}, Captures: []rules.Capture{}, Losses: []rules.Loss{}},
	}
	for round, w := range want {
		var res rules.Resolution
		s, _, _, res = rules.Resolve(s, round+1, 10, nil)
		if !reflect.DeepEqual(res, w) {
			t.Errorf("round %d: resolution = %+v, want %+v", round+1, res, w)
		}
	}
}

// A fight holds its elements: an element that starts the round in a cell
// with the enemy cannot move out, so the fight runs until one side is
// destroyed, and an element outside the fight still moves.
func TestAFightPinsItsElements(t *testing.T) {
	c := loc("a", 1, 1)
	s := state(force("r1", "red", 4, c), force("b1", "blue", 3, c), force("r2", "red", 2, loc("a", 3, 3)))
	orders := []rules.Order{order("b1", loc("a", 1, 2)), order("r1", loc("a", 1, 0)), order("r2", loc("a", 3, 2))}
	s, _, _, res := rules.Resolve(s, 1, 10, orders)
	at := map[string]rules.Location{}
	for _, e := range s.Elements {
		at[e.ID] = e.At
	}
	if at["r1"] != c || at["b1"] != c || at["r2"] != loc("a", 3, 2) {
		t.Errorf("after round 1: %v; want r1 and b1 pinned at %s and r2 moved", at, c.Key())
	}
	if len(res.Engagements) != 1 {
		t.Fatalf("round 1: engagements %+v, want the fight to go on", res.Engagements)
	}
	s, _, _, res = rules.Resolve(s, 2, 10, orders)
	if len(res.Losses) != 1 || res.Losses[0].ID != "b1" {
		t.Errorf("round 2: losses %+v, want b1", res.Losses)
	}
	// Once the fight is over, the survivor moves again.
	s, _, _, _ = rules.Resolve(s, 3, 10, []rules.Order{order("r1", loc("a", 1, 0))})
	for _, e := range s.Elements {
		if e.ID == "r1" && e.At != loc("a", 1, 0) {
			t.Errorf("r1 at %s after the fight, want a:1,0", e.At.Key())
		}
	}
}

// The resolution records an objective that changes hands, with the faction
// it was taken from, and none for one that stays with its holder.
func TestResolutionRecordsCaptures(t *testing.T) {
	obj := loc("a", 3, 0)
	s := state(force("r1", "red", 2, loc("a", 2, 0)))
	s, _, _, res := rules.Resolve(s, 1, 10, []rules.Order{order("r1", obj)})
	if want := []rules.Capture{{At: obj, Faction: "red"}}; !reflect.DeepEqual(res.Captures, want) {
		t.Errorf("captures = %+v, want %+v", res.Captures, want)
	}
	s.Elements = append(s.Elements, force("b1", "blue", 5, loc("a", 4, 0)))
	_, _, _, res = rules.Resolve(s, 2, 10, []rules.Order{order("b1", obj)})
	want := []rules.Capture{{At: obj, Faction: "blue", From: "red"}}
	if !reflect.DeepEqual(res.Captures, want) || !reflect.DeepEqual(res.Losses, []rules.Loss{{ID: "r1", Faction: "red"}}) {
		t.Errorf("resolution = %+v, want blue to take the objective from red, and r1 lost", res)
	}
	_, _, _, res = rules.Resolve(s, 2, 10, nil)
	if len(res.Captures) != 0 {
		t.Errorf("captures = %+v on a round nothing changed hands", res.Captures)
	}
}

func TestCapture(t *testing.T) {
	obj := loc("a", 3, 0)
	tests := []struct {
		name    string
		start   []rules.Element
		holders map[string]string
		orders  []rules.Order
		want    string
	}{
		{
			name:   "a faction alone on an objective captures it",
			start:  []rules.Element{force("r1", "red", 2, loc("a", 2, 0))},
			orders: []rules.Order{order("r1", obj)},
			want:   "red",
		},
		{
			name:    "a faction alone on an objective takes it from the holder",
			start:   []rules.Element{force("b1", "blue", 2, obj)},
			holders: map[string]string{"a:3,0": "red"},
			want:    "blue",
		},
		{
			name: "a tied engagement on an objective leaves its holder",
			start: []rules.Element{
				force("r1", "red", 2, obj), force("b1", "blue", 2, loc("a", 4, 0)),
			},
			holders: map[string]string{"a:3,0": "red"},
			orders:  []rules.Order{order("b1", obj)},
			want:    "red",
		},
		{
			name: "the survivor of an engagement on an objective captures it",
			start: []rules.Element{
				force("r1", "red", 2, obj), force("b1", "blue", 3, loc("a", 4, 0)),
			},
			holders: map[string]string{"a:3,0": "red"},
			orders:  []rules.Order{order("b1", obj)},
			want:    "blue",
		},
		{
			name:    "the holder persists when the objective is empty",
			start:   []rules.Element{force("r1", "red", 2, obj)},
			holders: map[string]string{"a:3,0": "red"},
			orders:  []rules.Order{order("r1", loc("a", 3, 1))},
			want:    "red",
		},
		{
			name:  "an objective no one has stood on stays unheld",
			start: []rules.Element{force("r1", "red", 2, loc("a", 0, 0))},
			want:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := state(tt.start...)
			if tt.holders != nil {
				s.Holders = tt.holders
			}
			next, _, _, _ := rules.Resolve(s, 1, 10, tt.orders)
			if got := next.Holders[obj.Key()]; got != tt.want {
				t.Errorf("holder = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveDoesNotMutateItsInput(t *testing.T) {
	s := state(
		force("r1", "red", 5, loc("a", 3, 1)),
		force("b1", "blue", 3, loc("a", 4, 0)),
		scout("r2", "red", loc("a", 0, 0)),
	)
	s.Holders["b:5,5"] = "blue"
	orders := []rules.Order{
		order("r1", loc("a", 3, 0)),
		order("b1", loc("a", 3, 0)),
		order("r2", loc("a", 1, 0), loc("a", 1, 1)),
	}
	before := deepCopy(s)
	ordersBefore := slices.Clone(orders)

	next, _, _, _ := rules.Resolve(s, 1, 10, orders)
	if !reflect.DeepEqual(s, before) {
		t.Errorf("input state changed:\n got %+v\nwant %+v", s, before)
	}
	if !reflect.DeepEqual(orders, ordersBefore) {
		t.Errorf("orders changed: %+v", orders)
	}

	// The next state shares nothing the caller can change through it.
	next.Holders["a:3,0"] = "blue"
	next.Elements[0].Strength = 99
	next.Map.Sectors[0].Objectives[0] = rules.Point{X: 0, Y: 5}
	if !reflect.DeepEqual(s, before) {
		t.Errorf("changing the next state changed the input")
	}
}

func deepCopy(s rules.State) rules.State {
	out := s
	out.Map.Sectors = nil
	for _, sec := range s.Map.Sectors {
		sec.Obstacles = slices.Clone(sec.Obstacles)
		sec.Objectives = slices.Clone(sec.Objectives)
		sec.Gates = slices.Clone(sec.Gates)
		out.Map.Sectors = append(out.Map.Sectors, sec)
	}
	out.Elements = slices.Clone(s.Elements)
	out.Holders = map[string]string{}
	for k, v := range s.Holders {
		out.Holders[k] = v
	}
	return out
}

// An idle exercise, with no orders and factions apart, runs every round to
// its limit without anything happening, and ends there as a draw.
func TestIdleExerciseRunsToTheLimitAsADraw(t *testing.T) {
	const limit = 5
	s := state(
		force("b1", "blue", 3, loc("b", 0, 5)),
		force("r1", "red", 3, loc("a", 0, 0)),
	)
	if v := rules.Judge(s, 0, limit); v.Over {
		t.Fatalf("round 0: %+v, want not over", v)
	}
	for round := 1; round <= limit; round++ {
		next, obs, v, _ := rules.Resolve(s, round, limit, nil)
		if !reflect.DeepEqual(next.Elements, s.Elements) {
			t.Fatalf("round %d: elements changed to %+v", round, next.Elements)
		}
		for i, o := range obs {
			if o.Round != round || o.Faction != s.Factions[i] || len(o.Contacts) != 0 {
				t.Errorf("round %d: observation %d = %+v", round, i, o)
			}
		}
		if round < limit && v.Over {
			t.Fatalf("round %d: %+v, want not over", round, v)
		}
		if round == limit && v != (rules.Verdict{Over: true, Winner: "", Reason: "limit"}) {
			t.Fatalf("round %d: %+v, want a draw at the limit", round, v)
		}
		s = next
	}
}
