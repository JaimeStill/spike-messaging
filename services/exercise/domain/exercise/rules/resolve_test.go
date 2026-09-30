package rules_test

import (
	"maps"
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
			name:   "a squad steps one cell",
			start:  []rules.Element{squad("r1", "red", loc("a", 0, 0))},
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
			start:  []rules.Element{squad("r1", "red", loc("a", 0, 0))},
			orders: []rules.Order{order("r1")},
			want:   map[string]rules.Location{"r1": loc("a", 0, 0)},
		},
		{
			name:   "a squad ordered two steps stays",
			start:  []rules.Element{squad("r1", "red", loc("a", 0, 0))},
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
			start:  []rules.Element{squad("r1", "red", loc("a", 0, 0))},
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
			start:  []rules.Element{squad("r1", "red", loc("a", 0, 0))},
			orders: []rules.Order{order("r1", loc("a", -1, 0))},
			want:   map[string]rules.Location{"r1": loc("a", 0, 0)},
		},
		{
			name:   "a step into an obstacle is refused",
			start:  []rules.Element{squad("r1", "red", loc("a", 2, 1))},
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
			start:  []rules.Element{squad("r1", "red", loc("a", 5, 4))},
			orders: []rules.Order{order("r1", loc("a", 5, 5))},
			want:   map[string]rules.Location{"r1": loc("a", 5, 5)},
		},
		{
			name:   "a step from a gate to its link traverses it",
			start:  []rules.Element{squad("r1", "red", loc("a", 5, 5))},
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
			start:  []rules.Element{squad("r1", "red", loc("a", 5, 5))},
			orders: []rules.Order{order("r1", loc("a", 4, 5))},
			want:   map[string]rules.Location{"r1": loc("a", 4, 5)},
		},
		{
			name:   "a step into another sector away from a gate is refused",
			start:  []rules.Element{squad("r1", "red", loc("a", 0, 0))},
			orders: []rules.Order{order("r1", loc("b", 0, 0))},
			want:   map[string]rules.Location{"r1": loc("a", 0, 0)},
		},
		{
			name:   "a step from a gate to a cell other than its link is refused",
			start:  []rules.Element{squad("r1", "red", loc("a", 5, 5))},
			orders: []rules.Order{order("r1", loc("b", 1, 0))},
			want:   map[string]rules.Location{"r1": loc("a", 5, 5)},
		},
		{
			name:   "an order for an unknown element is ignored",
			start:  []rules.Element{squad("r1", "red", loc("a", 0, 0))},
			orders: []rules.Order{order("zz", loc("a", 1, 0))},
			want:   map[string]rules.Location{"r1": loc("a", 0, 0)},
		},
		{
			name:  "the last order for an element wins",
			start: []rules.Element{squad("r1", "red", loc("a", 0, 0))},
			orders: []rules.Order{
				order("r1", loc("a", 1, 0)),
				order("r1", loc("a", 0, 1)),
			},
			want: map[string]rules.Location{"r1": loc("a", 0, 1)},
		},
		{
			name: "two elements of one faction ending on one cell are both refused",
			start: []rules.Element{
				squad("r1", "red", loc("a", 0, 1)),
				squad("r2", "red", loc("a", 1, 0)),
			},
			orders: []rules.Order{order("r1", loc("a", 1, 1)), order("r2", loc("a", 1, 1))},
			want:   map[string]rules.Location{"r1": loc("a", 0, 1), "r2": loc("a", 1, 0)},
		},
		{
			name: "moving onto a faction member that stays is refused",
			start: []rules.Element{
				squad("r1", "red", loc("a", 0, 0)),
				squad("r2", "red", loc("a", 1, 0)),
			},
			orders: []rules.Order{order("r1", loc("a", 1, 0))},
			want:   map[string]rules.Location{"r1": loc("a", 0, 0), "r2": loc("a", 1, 0)},
		},
		{
			name: "moving into a cell a faction member leaves is allowed",
			start: []rules.Element{
				squad("r1", "red", loc("a", 0, 0)),
				squad("r2", "red", loc("a", 1, 0)),
			},
			orders: []rules.Order{order("r1", loc("a", 1, 0)), order("r2", loc("a", 1, 1))},
			want:   map[string]rules.Location{"r1": loc("a", 1, 0), "r2": loc("a", 1, 1)},
		},
		{
			name: "a refusal cascades to the element that would have followed",
			start: []rules.Element{
				squad("r1", "red", loc("a", 0, 0)),
				squad("r2", "red", loc("a", 1, 0)),
				squad("r3", "red", loc("a", 1, 2)),
				squad("r4", "red", loc("a", 2, 1)),
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
				squad("r1", "red", loc("a", 0, 0)),
				squad("r2", "red", loc("a", 1, 0)),
			},
			orders: []rules.Order{order("r1", loc("a", 1, 0)), order("r2", loc("a", 0, 0))},
			want:   map[string]rules.Location{"r1": loc("a", 1, 0), "r2": loc("a", 0, 0)},
		},
		{
			name: "enemies swapping cells do not meet",
			start: []rules.Element{
				squad("r1", "red", loc("a", 0, 0)),
				squad("b1", "blue", loc("a", 1, 0)),
			},
			orders: []rules.Order{order("r1", loc("a", 1, 0)), order("b1", loc("a", 0, 0))},
			want:   map[string]rules.Location{"r1": loc("a", 1, 0), "b1": loc("a", 0, 0)},
		},
		{
			name: "a scout passing through an enemy's cell does not meet it",
			start: []rules.Element{
				scout("r1", "red", loc("a", 0, 0)),
				squad("b1", "blue", loc("a", 1, 0)),
			},
			orders: []rules.Order{order("r1", loc("a", 1, 0), loc("a", 2, 0))},
			want:   map[string]rules.Location{"r1": loc("a", 2, 0), "b1": loc("a", 1, 0)},
		},
		{
			name: "an engaged element's retreat of one step leaves the fight",
			start: []rules.Element{
				engaged(squad("r1", "red", loc("a", 1, 1))),
				engaged(squad("b1", "blue", loc("a", 1, 1))),
			},
			orders: []rules.Order{retreat("r1", loc("a", 1, 0))},
			want:   map[string]rules.Location{"r1": loc("a", 1, 0), "b1": loc("a", 1, 1)},
		},
		{
			name: "an engaged scout's retreat of two steps is refused",
			start: []rules.Element{
				engaged(scout("r1", "red", loc("a", 1, 1))),
				engaged(squad("b1", "blue", loc("a", 1, 1))),
			},
			orders: []rules.Order{retreat("r1", loc("a", 1, 0), loc("a", 0, 0))},
			want:   map[string]rules.Location{"r1": loc("a", 1, 1), "b1": loc("a", 1, 1)},
		},
		{
			name:   "a retreat by an element in no fight is an ordinary move",
			start:  []rules.Element{scout("r1", "red", loc("a", 0, 0))},
			orders: []rules.Order{retreat("r1", loc("a", 1, 0), loc("a", 1, 1))},
			want:   map[string]rules.Location{"r1": loc("a", 1, 1)},
		},
		{
			name:   "a recovering element's order is refused",
			start:  []rules.Element{recovering(squad("r1", "red", loc("a", 0, 0)))},
			orders: []rules.Order{order("r1", loc("a", 1, 0))},
			want:   map[string]rules.Location{"r1": loc("a", 0, 0)},
		},
		{
			name: "reinforcements join their own elements in a fight",
			start: []rules.Element{
				engaged(squad("r1", "red", loc("a", 1, 1))),
				engaged(squad("b1", "blue", loc("a", 1, 1))),
				squad("r2", "red", loc("a", 0, 1)),
				squad("r3", "red", loc("a", 1, 0)),
			},
			orders: []rules.Order{order("r2", loc("a", 1, 1)), order("r3", loc("a", 1, 1))},
			want: map[string]rules.Location{
				"r1": loc("a", 1, 1), "r2": loc("a", 1, 1), "r3": loc("a", 1, 1), "b1": loc("a", 1, 1),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := state(tt.start...)
			if err := s.Validate(); err != nil {
				t.Fatalf("start: %v", err)
			}
			next, _, _, res := rules.Resolve(s, seed, 1, 10, tt.orders)
			lost := map[string]bool{}
			for _, l := range res.Losses {
				lost[l.ID] = true
			}
			if len(next.Elements)+len(lost) != len(tt.want) {
				t.Fatalf("elements = %v and lost %v, want %d", ids(next.Elements), lost, len(tt.want))
			}
			// A fight may destroy an element where it stands; the test is of
			// where the survivors end.
			for id, want := range tt.want {
				e, ok := find(next, id)
				if !ok {
					if !lost[id] {
						t.Errorf("%s: missing", id)
					}
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

// A fight is random, but a seed and a round decide it: the same state,
// seed, round, and orders always resolve alike.
func TestAFightIsDecidedBySeedAndRound(t *testing.T) {
	c := loc("a", 1, 1)
	s := state(engaged(squad("r1", "red", c)), engaged(squad("b1", "blue", c)))
	first, _, _, res := rules.Resolve(s, seed, 1, 10, nil)
	again, _, _, resAgain := rules.Resolve(s, seed, 1, 10, nil)
	if !reflect.DeepEqual(first, again) || !reflect.DeepEqual(res, resAgain) {
		t.Errorf("one seed and round resolved two ways:\n%+v\n%+v", res, resAgain)
	}
	differ := false
	for other := int64(1); other <= 20 && !differ; other++ {
		_, _, _, r := rules.Resolve(s, other, 1, 10, nil)
		differ = !reflect.DeepEqual(r, res)
	}
	if !differ {
		t.Errorf("twenty seeds resolved round 1 alike")
	}
}

// Every living operator fires once, all at once: a side's loss in a round
// is at most its enemy's operators times the most a hit takes, and each
// element's strength stays the sum of its health.
func TestFireIsBoundedAndSimultaneous(t *testing.T) {
	c := loc("a", 1, 1)
	for sd := int64(1); sd <= 50; sd++ {
		s := state(
			engaged(squad("r1", "red", c)),
			engaged(scout("b1", "blue", c)),
			engaged(hurt(squad("b2", "blue", c), 30, 30)),
		)
		next, _, _, res := rules.Resolve(s, sd, 1, 10, nil)
		lost := map[string]int{}
		for _, g := range res.Engagements {
			for _, e := range g.Elements {
				lost[e.Faction] += e.Before - e.After
				if e.After > e.Before || e.Fallen < 0 {
					t.Fatalf("seed %d: %+v", sd, e)
				}
			}
		}
		if lost["red"] > 3*rules.MaxDamage || lost["blue"] > 4*rules.MaxDamage {
			t.Errorf("seed %d: losses %v exceed what the operators can fire", sd, lost)
		}
		if err := next.Validate(); err != nil {
			t.Fatalf("seed %d: %v", sd, err)
		}
	}
}

// A fight lasts while both sides stay in the cell: two full squads take
// several rounds. Each round's resolution follows on from the last, the
// elements stay engaged until one side falls, and the fallen one is
// recorded as a loss in the round it happens.
func TestAFightLastsRounds(t *testing.T) {
	c := loc("a", 1, 1)
	s := state(squad("r1", "red", c), squad("b1", "blue", c))
	strength := map[string]int{"r1": 400, "b1": 400}
	rounds := 0
	for round := 1; round <= 30; round++ {
		var res rules.Resolution
		s, _, _, res = rules.Resolve(s, seed, round, 50, nil)
		if len(res.Engagements) == 0 {
			break
		}
		rounds++
		for _, e := range res.Engagements[0].Elements {
			if e.Before != strength[e.ID] {
				t.Fatalf("round %d: %s before %d, want %d", round, e.ID, e.Before, strength[e.ID])
			}
			strength[e.ID] = e.After
		}
		for _, e := range s.Elements {
			if len(res.Losses) == 0 && e.Status != rules.StatusEngaged {
				t.Errorf("round %d: %s is %s, want engaged", round, e.ID, e.Status)
			}
		}
		if len(res.Losses) > 0 {
			if len(s.Elements) > 1 {
				t.Errorf("round %d: losses %+v but %d elements left", round, res.Losses, len(s.Elements))
			}
			for _, e := range s.Elements {
				if e.Status != rules.StatusReady {
					t.Errorf("round %d: the survivor %s is %s, want ready", round, e.ID, e.Status)
				}
			}
		}
	}
	if rounds < 3 {
		t.Errorf("the fight lasted %d rounds, want several", rounds)
	}
	if len(s.Elements) > 1 {
		t.Errorf("the fight did not end in 30 rounds: %+v", s.Elements)
	}
}

// A fight holds its elements: an element that starts the round in a cell
// with the enemy cannot move out but by a retreat. An element outside the
// fight still moves.
func TestAFightPinsItsElements(t *testing.T) {
	c := loc("a", 1, 1)
	s := state(engaged(squad("r1", "red", c)), engaged(squad("b1", "blue", c)), squad("r2", "red", loc("a", 3, 3)))
	orders := []rules.Order{order("b1", loc("a", 1, 2)), order("r1", loc("a", 1, 0)), order("r2", loc("a", 3, 2))}
	s, _, _, res := rules.Resolve(s, seed, 1, 10, orders)
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
}

// A retreat no one pursues leaves the fight by one step without a shot,
// and costs the element its next round, after which it is ready again.
func TestAnUnpursuedRetreatEscapes(t *testing.T) {
	c, back := loc("a", 1, 1), loc("a", 1, 0)
	s := state(engaged(squad("r1", "red", c)), engaged(squad("b1", "blue", c)))
	s, _, _, res := rules.Resolve(s, seed, 1, 10, []rules.Order{retreat("r1", back)})
	want := []rules.Retreat{{ID: "r1", Faction: "red", From: c, To: back, Before: 400, After: 400, Pursuers: []rules.Engaged{}}}
	if !reflect.DeepEqual(res.Retreats, want) {
		t.Fatalf("retreats = %+v, want %+v", res.Retreats, want)
	}
	if len(res.Engagements) != 0 {
		t.Errorf("engagements = %+v, want the fight broken off", res.Engagements)
	}
	r1, _ := find(s, "r1")
	b1, _ := find(s, "b1")
	if r1.At != back || r1.Status != rules.StatusRecovering || r1.Strength != 400 {
		t.Errorf("r1 = %+v, want recovering at %s, untouched", r1, back.Key())
	}
	if b1.Strength != 400 || b1.Status != rules.StatusReady {
		t.Errorf("b1 = %+v, want untouched and ready", b1)
	}

	s, _, _, _ = rules.Resolve(s, seed, 2, 10, []rules.Order{order("r1", loc("a", 0, 0))})
	r1, _ = find(s, "r1")
	if r1.At != back || r1.Status != rules.StatusReady {
		t.Errorf("round 2: r1 = %+v, want it held at %s and ready", r1, back.Key())
	}
	s, _, _, _ = rules.Resolve(s, seed, 3, 10, []rules.Order{order("r1", loc("a", 0, 0))})
	if r1, _ = find(s, "r1"); r1.At != loc("a", 0, 0) {
		t.Errorf("round 3: r1 at %s, want it moving again", r1.At.Key())
	}
}

// A pursued retreat trades fire: the pursuer fires on the retreating
// element, which fires back, and the record holds both sides' losses. Over
// many seeds, each side takes hits.
func TestAPursuedRetreatTradesFire(t *testing.T) {
	c, back := loc("a", 1, 1), loc("a", 1, 0)
	var hurtRetreat, hurtPursuer bool
	for sd := int64(1); sd <= 50; sd++ {
		s := state(engaged(squad("r1", "red", c)), engaged(squad("b1", "blue", c)))
		next, _, _, res := rules.Resolve(s, sd, 1, 10, []rules.Order{retreat("r1", back), pursue("b1")})
		if len(res.Retreats) != 1 || len(res.Retreats[0].Pursuers) != 1 {
			t.Fatalf("seed %d: retreats = %+v, want r1 pursued by b1", sd, res.Retreats)
		}
		r, p := res.Retreats[0], res.Retreats[0].Pursuers[0]
		if p.ID != "b1" || p.Before != 400 || r.Before != 400 {
			t.Fatalf("seed %d: %+v", sd, r)
		}
		hurtRetreat = hurtRetreat || r.After < r.Before
		hurtPursuer = hurtPursuer || p.After < p.Before
		b1, _ := find(next, "b1")
		if b1.Strength != p.After {
			t.Errorf("seed %d: b1 strength %d, want the record's %d", sd, b1.Strength, p.After)
		}
		if err := next.Validate(); err != nil {
			t.Fatalf("seed %d: %v", sd, err)
		}
	}
	if !hurtRetreat || !hurtPursuer {
		t.Errorf("fifty seeds: the retreat hurt %v, the pursuer hurt %v; want both", hurtRetreat, hurtPursuer)
	}
}

// A pursuit can destroy the element that retreats, and the loss is recorded.
func TestAPursuitCanDestroyARetreat(t *testing.T) {
	c := loc("a", 1, 1)
	for sd := int64(1); sd <= 50; sd++ {
		s := state(engaged(hurt(scout("r1", "red", c), 1)), engaged(squad("b1", "blue", c)))
		next, _, _, res := rules.Resolve(s, sd, 1, 10, []rules.Order{retreat("r1", loc("a", 1, 0)), pursue("b1")})
		if len(res.Losses) == 0 {
			continue
		}
		if _, ok := find(next, "r1"); ok || res.Retreats[0].After != 0 || res.Retreats[0].Fallen != 1 {
			t.Errorf("seed %d: %+v, want r1 destroyed by the pursuit", sd, res)
		}
		return
	}
	t.Errorf("fifty seeds, and no pursuit hit a scout at health 1")
}

// Only an element that stood in the cell at the round's start, was not
// recovering, and stays there pursues: one that steps into the cell, or a
// recovering one, does not fire on the retreat, whatever its order says.
func TestOnlyAStayingElementPursues(t *testing.T) {
	c, back := loc("a", 1, 1), loc("a", 1, 0)
	s := state(
		engaged(squad("r1", "red", c)),
		engaged(squad("b1", "blue", c)),
		recovering(squad("b2", "blue", c)),
		squad("b3", "blue", loc("a", 2, 1)),
	)
	arrive := rules.Order{Element: "b3", Steps: []rules.Location{c}, Pursue: true}
	_, _, _, res := rules.Resolve(s, seed, 1, 10, []rules.Order{retreat("r1", back), pursue("b1"), pursue("b2"), arrive})
	if len(res.Retreats) != 1 {
		t.Fatalf("retreats = %+v, want r1's", res.Retreats)
	}
	var got []string
	for _, p := range res.Retreats[0].Pursuers {
		got = append(got, p.ID)
	}
	if !reflect.DeepEqual(got, []string{"b1"}) {
		t.Errorf("pursuers = %v, want only b1", got)
	}
}

func TestCapture(t *testing.T) {
	obj := loc("a", 3, 0)
	tests := []struct {
		name    string
		start   []rules.Element
		holders map[string]string
		rounds  [][]rules.Order
		want    string
	}{
		{
			name:   "one round alone on an objective does not take it",
			start:  []rules.Element{squad("r1", "red", loc("a", 2, 0))},
			rounds: [][]rules.Order{{order("r1", obj)}},
			want:   "",
		},
		{
			name:   "two rounds alone on an objective take it",
			start:  []rules.Element{squad("r1", "red", loc("a", 2, 0))},
			rounds: [][]rules.Order{{order("r1", obj)}, nil},
			want:   "red",
		},
		{
			name:    "two rounds alone take it from its holder",
			start:   []rules.Element{squad("b1", "blue", obj)},
			holders: map[string]string{"a:3,0": "red"},
			rounds:  [][]rules.Order{nil, nil},
			want:    "blue",
		},
		{
			name:   "an empty round resets the count",
			start:  []rules.Element{squad("r1", "red", loc("a", 2, 0))},
			rounds: [][]rules.Order{{order("r1", obj)}, {order("r1", loc("a", 2, 0))}, {order("r1", obj)}},
			want:   "",
		},
		{
			name: "a fight resets the count, and leaves the holder",
			start: []rules.Element{
				squad("r1", "red", obj), squad("b1", "blue", loc("a", 4, 0)),
			},
			holders: map[string]string{"a:3,0": "red"},
			rounds:  [][]rules.Order{{order("b1", obj)}, nil},
			want:    "red",
		},
		{
			name:    "the holder persists when the objective is empty",
			start:   []rules.Element{squad("r1", "red", obj)},
			holders: map[string]string{"a:3,0": "red"},
			rounds:  [][]rules.Order{{order("r1", loc("a", 3, 1))}, nil},
			want:    "red",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := state(tt.start...)
			if tt.holders != nil {
				s.Holders = tt.holders
			}
			for i, orders := range tt.rounds {
				s, _, _, _ = rules.Resolve(s, seed, i+1, 10, orders)
			}
			if got := s.Holders[obj.Key()]; got != tt.want {
				t.Errorf("holder = %q, want %q", got, tt.want)
			}
		})
	}
}

// The resolution records a faction's count toward an objective, then the
// capture with the faction it was taken from, and nothing once it is held.
func TestResolutionRecordsProgressAndCaptures(t *testing.T) {
	obj := loc("a", 3, 0)
	s := state(squad("b1", "blue", loc("a", 2, 0)))
	s.Holders[obj.Key()] = "red"
	s, _, _, res := rules.Resolve(s, seed, 1, 10, []rules.Order{order("b1", obj)})
	if want := []rules.Advance{{At: obj, Faction: "blue", Rounds: 1}}; !reflect.DeepEqual(res.Progress, want) || len(res.Captures) != 0 {
		t.Errorf("round 1: %+v, want blue one round toward %s", res, obj.Key())
	}
	if want := (rules.Progress{Faction: "blue", Rounds: 1}); s.Progress[obj.Key()] != want {
		t.Errorf("round 1: state progress %+v, want %+v", s.Progress, want)
	}
	s, _, _, res = rules.Resolve(s, seed, 2, 10, nil)
	if want := []rules.Capture{{At: obj, Faction: "blue", From: "red"}}; !reflect.DeepEqual(res.Captures, want) || len(res.Progress) != 0 {
		t.Errorf("round 2: %+v, want blue to take %s from red", res, obj.Key())
	}
	if len(s.Progress) != 0 {
		t.Errorf("round 2: state progress %+v, want none once held", s.Progress)
	}
	_, _, _, res = rules.Resolve(s, seed, 3, 10, nil)
	if len(res.Captures) != 0 || len(res.Progress) != 0 {
		t.Errorf("round 3: %+v on a round nothing changed", res)
	}
}

func TestResolveDoesNotMutateItsInput(t *testing.T) {
	c := loc("a", 3, 0)
	s := state(
		engaged(squad("r1", "red", c)),
		engaged(squad("b1", "blue", c)),
		scout("r2", "red", loc("a", 0, 0)),
	)
	s.Holders["b:5,5"] = "blue"
	s.Progress["a:3,0"] = rules.Progress{Faction: "red", Rounds: 1}
	orders := []rules.Order{
		order("r2", loc("a", 1, 0), loc("a", 1, 1)),
	}
	before := deepCopy(s)
	ordersBefore := slices.Clone(orders)

	next, _, _, _ := rules.Resolve(s, seed, 1, 10, orders)
	if !reflect.DeepEqual(s, before) {
		t.Errorf("input state changed:\n got %+v\nwant %+v", s, before)
	}
	if !reflect.DeepEqual(orders, ordersBefore) {
		t.Errorf("orders changed: %+v", orders)
	}

	// The next state shares nothing the caller can change through it.
	next.Holders["a:3,0"] = "blue"
	next.Progress["b:5,5"] = rules.Progress{Faction: "red", Rounds: 1}
	next.Elements[0].Health[0] = 99
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
	out.Elements = nil
	for _, e := range s.Elements {
		e.Health = slices.Clone(e.Health)
		out.Elements = append(out.Elements, e)
	}
	out.Holders = maps.Clone(s.Holders)
	out.Progress = maps.Clone(s.Progress)
	return out
}

// An idle exercise, with no orders and factions apart, runs every round to
// its limit without anything happening, and ends there as a draw.
func TestIdleExerciseRunsToTheLimitAsADraw(t *testing.T) {
	const limit = 5
	s := state(
		squad("b1", "blue", loc("b", 0, 5)),
		squad("r1", "red", loc("a", 0, 0)),
	)
	if v := rules.Judge(s, 0, limit); v.Over {
		t.Fatalf("round 0: %+v, want not over", v)
	}
	for round := 1; round <= limit; round++ {
		next, obs, v, _ := rules.Resolve(s, seed, round, limit, nil)
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
