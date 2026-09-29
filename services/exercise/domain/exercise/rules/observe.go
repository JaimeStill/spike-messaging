package rules

import (
	"cmp"
	"slices"
)

// Observation is what one faction sees after a round: its own elements,
// the enemy elements its elements can see, and the objectives in their
// sight with who holds them. Every list is sorted: elements by ID and
// objectives by [Location.Key].
type Observation struct {
	Faction    string            `json:"faction"`
	Round      int               `json:"round"`
	Own        []Element         `json:"own"`
	Contacts   []Element         `json:"contacts"`
	Objectives []ObjectiveStatus `json:"objectives"`
}

// ObjectiveStatus is an objective a faction sees, and the faction holding
// it, or "" when it is unheld.
type ObjectiveStatus struct {
	At     Location `json:"at"`
	Holder string   `json:"holder"`
}

// Observe returns each faction's observation of s after round, indexed like
// s.Factions. An element sees every cell of its own sector within the
// Chebyshev distance of its [Kind.Sight]; no element sees into another
// sector, even through a gate. The exercise's start is observed as round 0.
func Observe(s State, round int) [2]Observation {
	var out [2]Observation
	for i, f := range s.Factions {
		o := Observation{Faction: f, Round: round, Own: []Element{}, Contacts: []Element{}, Objectives: []ObjectiveStatus{}}
		var own []Element
		for _, e := range s.Elements {
			if e.Faction == f {
				own = append(own, e)
			}
		}
		o.Own = append(o.Own, own...)
		for _, e := range s.Elements {
			if e.Faction != f && seen(own, e.At) {
				o.Contacts = append(o.Contacts, e)
			}
		}
		for _, at := range s.Map.objectives() {
			if seen(own, at) {
				o.Objectives = append(o.Objectives, ObjectiveStatus{At: at, Holder: s.Holders[at.Key()]})
			}
		}
		byID := func(a, b Element) int { return cmp.Compare(a.ID, b.ID) }
		slices.SortFunc(o.Own, byID)
		slices.SortFunc(o.Contacts, byID)
		slices.SortFunc(o.Objectives, func(a, b ObjectiveStatus) int { return cmp.Compare(a.At.Key(), b.At.Key()) })
		out[i] = o
	}
	return out
}

// seen reports whether any of es sees at.
func seen(es []Element, at Location) bool {
	for _, e := range es {
		if e.At.Sector == at.Sector && max(abs(e.At.X-at.X), abs(e.At.Y-at.Y)) <= e.Kind.Sight() {
			return true
		}
	}
	return false
}
