package rules

import (
	"cmp"
	"maps"
	"slices"
)

// Order is a faction's command to one element for one round: the cells it
// enters, in order. An order with no steps holds the element in place.
type Order struct {
	Element string     `json:"element"`
	Steps   []Location `json:"steps"`
}

// Resolve resolves round of s under orders: it moves, engages, and
// captures, then observes the next state for each faction and judges it
// against limit, the exercise's round limit. It returns the next state, the
// observations indexed like s.Factions, the verdict, and the round's
// [Resolution]. Resolve does not change s or orders; the next state shares
// no slice or map with s, and its elements are sorted by ID.
func Resolve(s State, round, limit int, orders []Order) (next State, obs [2]Observation, v Verdict, res Resolution) {
	next = s.clone()
	move(&next, orders)
	res.Engagements, res.Losses = engage(&next)
	res.Captures = capture(&next)
	return next, Observe(next, round), Judge(next, round, limit), res
}

// contested returns the cells, by [Location.Key], that hold both factions'
// elements.
func contested(es []Element) map[string]bool {
	factions := make(map[string]map[string]bool)
	for _, e := range es {
		k := e.At.Key()
		if factions[k] == nil {
			factions[k] = make(map[string]bool)
		}
		factions[k][e.Faction] = true
	}
	out := make(map[string]bool)
	for k, fs := range factions {
		if len(fs) > 1 {
			out[k] = true
		}
	}
	return out
}

// Resolution is what a round's resolution did, as the umpire records it:
// each engagement, each objective that changed hands, and each element
// destroyed. Every list is empty, not nil, for a round where nothing
// happened.
type Resolution struct {
	Engagements []Engagement `json:"engagements"`
	Captures    []Capture    `json:"captures"`
	Losses      []Loss       `json:"losses"`
}

// Engagement is one cell's fight in a round: every element in the cell, of
// both factions.
type Engagement struct {
	At       Location  `json:"at"`
	Elements []Engaged `json:"elements"`
}

// Engaged is one element of an [Engagement]: its strength before and after
// the round's losses. After is 0 when the element was destroyed.
type Engaged struct {
	ID      string `json:"id"`
	Faction string `json:"faction"`
	Before  int    `json:"before"`
	After   int    `json:"after"`
}

// Capture is an objective that changed hands. Faction holds it now, and From
// held it before ("" when it was unheld).
type Capture struct {
	At      Location `json:"at"`
	Faction string   `json:"faction"`
	From    string   `json:"from"`
}

// Loss is an element destroyed in an engagement.
type Loss struct {
	ID      string `json:"id"`
	Faction string `json:"faction"`
}

// move applies every order at once. It refuses the orders the rules refuse
// and every order of an element pinned in a fight.
func move(s *State, orders []Order) {
	byID := make(map[string]Order, len(orders))
	for _, o := range orders {
		byID[o.Element] = o
	}
	pinned := contested(s.Elements)
	final := make([]Location, len(s.Elements))
	for i, e := range s.Elements {
		final[i] = e.At
		if o, ok := byID[e.ID]; ok && !pinned[e.At.Key()] {
			if to, ok := s.Map.walk(e, o.Steps); ok {
				final[i] = to
			}
		}
	}
	for refused := true; refused; {
		refused = false
		ends := make(map[string]int, len(final))
		for i, e := range s.Elements {
			ends[e.Faction+"@"+final[i].Key()]++
		}
		for i, e := range s.Elements {
			if final[i] != e.At && ends[e.Faction+"@"+final[i].Key()] > 1 {
				final[i] = e.At
				refused = true
			}
		}
	}
	for i := range s.Elements {
		s.Elements[i].At = final[i]
	}
}

// walk returns where e ends after steps, or false when the rules refuse the
// order: more steps than e's kind allows, or a step that is neither an
// orthogonal adjacency in the sector nor a traversal of the gate e stands
// on, or that leaves the grid or enters an obstacle.
func (m Map) walk(e Element, steps []Location) (Location, bool) {
	if len(steps) > e.Kind.Moves() {
		return Location{}, false
	}
	at := e.At
	for _, to := range steps {
		sec, ok := m.sector(at.Sector)
		if !ok {
			return Location{}, false
		}
		if g, ok := sec.gate(at.Point); ok && to == g.To {
			sec, ok = m.sector(to.Sector)
			if !ok {
				return Location{}, false
			}
		} else if to.Sector != at.Sector || abs(to.X-at.X)+abs(to.Y-at.Y) != 1 {
			return Location{}, false
		}
		if !sec.inside(to.Point) || sec.obstacle(to.Point) {
			return Location{}, false
		}
		at = to
	}
	return at, true
}

// engage fights out every cell that holds both factions' elements for one
// round, by attrition: each faction loses half the enemy's total strength
// in the cell, rounded up, and both losses apply at once. It leaves s's
// elements sorted by ID, and returns each cell's engagement, in cell order,
// and the elements destroyed, in ID order.
func engage(s *State) ([]Engagement, []Loss) {
	cells := make(map[string][]Element)
	for _, e := range s.Elements {
		cells[e.At.Key()] = append(cells[e.At.Key()], e)
	}
	engagements, losses := []Engagement{}, []Loss{}
	var live []Element
	for _, key := range sortedKeys(cells) {
		here := cells[key]
		var mine, theirs []Element
		var own, enemy int
		for _, e := range here {
			if e.Faction == s.Factions[0] {
				mine, own = append(mine, e), own+e.Strength
			} else {
				theirs, enemy = append(theirs, e), enemy+e.Strength
			}
		}
		if len(mine) == 0 || len(theirs) == 0 {
			live = append(live, here...)
			continue
		}
		survivors := append(absorb(mine, (enemy+1)/2), absorb(theirs, (own+1)/2)...)
		after := make(map[string]int, len(survivors))
		for _, e := range survivors {
			after[e.ID] = e.Strength
		}
		g := Engagement{At: here[0].At}
		slices.SortFunc(here, func(a, b Element) int { return cmp.Compare(a.ID, b.ID) })
		for _, e := range here {
			g.Elements = append(g.Elements, Engaged{ID: e.ID, Faction: e.Faction, Before: e.Strength, After: after[e.ID]})
			if after[e.ID] == 0 {
				losses = append(losses, Loss{ID: e.ID, Faction: e.Faction})
			}
		}
		engagements = append(engagements, g)
		live = append(live, survivors...)
	}
	slices.SortFunc(live, func(a, b Element) int { return cmp.Compare(a.ID, b.ID) })
	slices.SortFunc(losses, func(a, b Loss) int { return cmp.Compare(a.ID, b.ID) })
	s.Elements = live
	return engagements, losses
}

// absorb takes loss from es, weakest element first, and returns the
// elements that survive it.
func absorb(es []Element, loss int) []Element {
	slices.SortFunc(es, func(a, b Element) int {
		return cmp.Or(cmp.Compare(a.Strength, b.Strength), cmp.Compare(a.ID, b.ID))
	})
	var out []Element
	for _, e := range es {
		take := min(loss, e.Strength)
		e.Strength -= take
		loss -= take
		if e.Strength > 0 {
			out = append(out, e)
		}
	}
	return out
}

// capture gives each objective held by exactly one faction's elements to
// that faction, and returns each objective that changed hands, in the map's
// objective order.
func capture(s *State) []Capture {
	on := make(map[string]map[string]bool)
	for _, e := range s.Elements {
		k := e.At.Key()
		if on[k] == nil {
			on[k] = make(map[string]bool)
		}
		on[k][e.Faction] = true
	}
	captures := []Capture{}
	for _, o := range s.Map.objectives() {
		if fs := on[o.Key()]; len(fs) == 1 {
			for f := range fs {
				if from := s.Holders[o.Key()]; from != f {
					captures = append(captures, Capture{At: o, Faction: f, From: from})
				}
				s.Holders[o.Key()] = f
			}
		}
	}
	return captures
}

// sortedKeys returns m's keys in ascending order.
func sortedKeys[V any](m map[string]V) []string {
	return slices.Sorted(maps.Keys(m))
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
