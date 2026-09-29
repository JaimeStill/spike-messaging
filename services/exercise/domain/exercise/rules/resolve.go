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
// observations indexed like s.Factions, and the verdict. Resolve does not
// change s or orders; the next state shares no slice or map with s, and its
// elements are sorted by ID.
func Resolve(s State, round, limit int, orders []Order) (next State, obs [2]Observation, v Verdict) {
	next = s.clone()
	move(&next, orders)
	engage(&next)
	capture(&next)
	return next, Observe(next, round), Judge(next, round, limit)
}

// move applies every order at once, refusing the ones the rules refuse.
func move(s *State, orders []Order) {
	byID := make(map[string]Order, len(orders))
	for _, o := range orders {
		byID[o.Element] = o
	}
	final := make([]Location, len(s.Elements))
	for i, e := range s.Elements {
		final[i] = e.At
		if o, ok := byID[e.ID]; ok {
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

// engage fights out every cell that holds both factions' elements, and
// leaves s's elements sorted by ID.
func engage(s *State) {
	cells := make(map[string][]Element)
	for _, e := range s.Elements {
		cells[e.At.Key()] = append(cells[e.At.Key()], e)
	}
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
		switch {
		case len(mine) == 0 || len(theirs) == 0:
			live = append(live, here...)
		case own > enemy:
			live = append(live, absorb(mine, enemy)...)
		case enemy > own:
			live = append(live, absorb(theirs, own)...)
		}
	}
	slices.SortFunc(live, func(a, b Element) int { return cmp.Compare(a.ID, b.ID) })
	s.Elements = live
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
// that faction.
func capture(s *State) {
	on := make(map[string]map[string]bool)
	for _, e := range s.Elements {
		k := e.At.Key()
		if on[k] == nil {
			on[k] = make(map[string]bool)
		}
		on[k][e.Faction] = true
	}
	for _, o := range s.Map.objectives() {
		if fs := on[o.Key()]; len(fs) == 1 {
			for f := range fs {
				s.Holders[o.Key()] = f
			}
		}
	}
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
