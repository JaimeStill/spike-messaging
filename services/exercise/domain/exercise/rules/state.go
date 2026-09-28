package rules

import (
	"errors"
	"fmt"
	"maps"
)

// Element is a faction's piece on the map. A scout's strength is always 1;
// a force's is set by the exercise, and engagements reduce it.
type Element struct {
	ID       string   `json:"id"`
	Faction  string   `json:"faction"`
	Kind     Kind     `json:"kind"`
	Strength int      `json:"strength"`
	At       Location `json:"at"`
}

// State is an exercise between rounds: its map, its two factions, their
// live elements, and who holds which objective. An element that is
// destroyed is removed from Elements. Holders maps an objective's
// [Location.Key] to the faction holding it; an unheld objective has no
// entry.
type State struct {
	Map      Map               `json:"map"`
	Factions [2]string         `json:"factions"`
	Elements []Element         `json:"elements"`
	Holders  map[string]string `json:"holders"`
}

// Validate reports every way s breaks the state's rules: its map is valid;
// its two factions are distinct and named; element IDs are unique and
// non-empty; each element belongs to one of the factions, is a force of
// strength at least 1 or a scout of strength 1, and stands on an open or
// featured cell of an existing sector that is not an obstacle; no two
// elements of one faction share a cell; and every holder is a faction
// holding an objective's cell.
func (s State) Validate() error {
	var errs []error
	if err := s.Map.Validate(); err != nil {
		errs = append(errs, err)
	}
	a, b := s.Factions[0], s.Factions[1]
	if a == "" || b == "" {
		errs = append(errs, fmt.Errorf("factions %q and %q: a faction is unnamed", a, b))
	} else if a == b {
		errs = append(errs, fmt.Errorf("factions %q and %q: the factions are the same", a, b))
	}
	ids := make(map[string]bool, len(s.Elements))
	cells := make(map[string]string, len(s.Elements))
	for i, e := range s.Elements {
		name := fmt.Sprintf("element %q", e.ID)
		switch {
		case e.ID == "":
			name = fmt.Sprintf("element %d", i)
			errs = append(errs, fmt.Errorf("%s: an empty ID", name))
		case ids[e.ID]:
			errs = append(errs, fmt.Errorf("%s: a duplicate ID", name))
		}
		ids[e.ID] = true
		if !s.isFaction(e.Faction) {
			errs = append(errs, fmt.Errorf("%s: faction %q is not in the exercise", name, e.Faction))
		}
		switch e.Kind {
		case Force:
			if e.Strength < 1 {
				errs = append(errs, fmt.Errorf("%s: a force of strength %d, want at least 1", name, e.Strength))
			}
		case Scout:
			if e.Strength != 1 {
				errs = append(errs, fmt.Errorf("%s: a scout of strength %d, want 1", name, e.Strength))
			}
		default:
			errs = append(errs, fmt.Errorf("%s: kind %q is neither force nor scout", name, e.Kind))
		}
		if sec, ok := s.Map.sector(e.At.Sector); !ok {
			errs = append(errs, fmt.Errorf("%s: at %s, in a sector that does not exist", name, e.At.Key()))
		} else if !sec.inside(e.At.Point) {
			errs = append(errs, fmt.Errorf("%s: at %s, outside the grid", name, e.At.Key()))
		} else if sec.obstacle(e.At.Point) {
			errs = append(errs, fmt.Errorf("%s: at %s, on an obstacle", name, e.At.Key()))
		}
		cell := e.Faction + "@" + e.At.Key()
		if other, ok := cells[cell]; ok {
			errs = append(errs, fmt.Errorf("%s: at %s, a cell element %q of its faction holds", name, e.At.Key(), other))
		} else {
			cells[cell] = e.ID
		}
	}
	objectives := make(map[string]bool)
	for _, o := range s.Map.objectives() {
		objectives[o.Key()] = true
	}
	for _, key := range sortedKeys(s.Holders) {
		if !objectives[key] {
			errs = append(errs, fmt.Errorf("holder of %s: not an objective", key))
		}
		if f := s.Holders[key]; !s.isFaction(f) {
			errs = append(errs, fmt.Errorf("holder of %s: faction %q is not in the exercise", key, f))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("rules: state: %w", errors.Join(errs...))
	}
	return nil
}

// isFaction reports whether f names one of s's factions.
func (s State) isFaction(f string) bool {
	_, ok := s.index(f)
	return ok
}

// clone returns a copy of s that shares no slice or map with it.
func (s State) clone() State {
	out := s
	out.Map = s.Map.clone()
	out.Elements = append([]Element(nil), s.Elements...)
	out.Holders = maps.Clone(s.Holders)
	if out.Holders == nil {
		out.Holders = map[string]string{}
	}
	return out
}
