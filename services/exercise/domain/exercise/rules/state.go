package rules

import (
	"errors"
	"fmt"
	"maps"
)

// Element is a faction's piece on the map. Health holds one percentage for
// each living operator, in the order the element fields them; an operator
// that falls drops out. Strength is always the sum of Health. Status is
// the element's condition after the last round.
type Element struct {
	ID       string   `json:"id"`
	Faction  string   `json:"faction"`
	Kind     Kind     `json:"kind"`
	Strength int      `json:"strength"`
	Health   []int    `json:"health"`
	Status   Status   `json:"status"`
	At       Location `json:"at"`
}

// Status is an element's condition after a round.
type Status string

// The statuses of an element.
const (
	// StatusReady is an element free to act on its next order.
	StatusReady Status = "ready"
	// StatusEngaged is an element in a cell that holds the other faction's
	// elements: it is pinned in the fight there, and only a retreat moves
	// it.
	StatusEngaged Status = "engaged"
	// StatusRecovering is an element that retreated in the last round: its next
	// order is refused.
	StatusRecovering Status = "recovering"
)

// Progress is a faction's count toward taking an objective: the rounds in
// a row it has ended alone on it.
type Progress struct {
	Faction string `json:"faction"`
	Rounds  int    `json:"rounds"`
}

// State is an exercise between rounds: its map, its two factions, their
// live elements, who holds which objective, and each faction's progress
// toward the objectives it is taking. An element that is destroyed is
// removed from Elements. Holders maps an objective's [Location.Key] to the
// faction holding it; an unheld objective has no entry. Progress maps an
// objective's key to a count below [CaptureRounds] by a faction that does
// not hold it.
type State struct {
	Map      Map                 `json:"map"`
	Factions [2]string           `json:"factions"`
	Elements []Element           `json:"elements"`
	Holders  map[string]string   `json:"holders"`
	Progress map[string]Progress `json:"progress"`
}

// Validate reports every way s breaks the state's rules: its map is valid;
// its two factions are distinct and named; element IDs are unique and
// non-empty; each element belongs to one of the factions, is a squad or a
// scout, fields between one operator and its kind's [Kind.Operators], each
// with health in 1–100, has the sum of its health as its strength, has a
// known status, and stands inside the grid of an existing sector on a cell
// that is not an obstacle; and every entry in Holders and Progress keys an
// objective and names one of the factions. Elements of one faction may
// share a cell, as reinforcements in a fight do.
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
		if n := e.Kind.Operators(); n == 0 {
			errs = append(errs, fmt.Errorf("%s: kind %q is neither squad nor scout", name, e.Kind))
		} else if len(e.Health) < 1 || len(e.Health) > n {
			errs = append(errs, fmt.Errorf("%s: %d operators, want 1 to %d", name, len(e.Health), n))
		}
		sum := 0
		for _, h := range e.Health {
			if h < 1 || h > 100 {
				errs = append(errs, fmt.Errorf("%s: an operator at health %d, want 1 to 100", name, h))
			}
			sum += h
		}
		if e.Strength != sum {
			errs = append(errs, fmt.Errorf("%s: strength %d, want %d, the sum of its health", name, e.Strength, sum))
		}
		switch e.Status {
		case StatusReady, StatusEngaged, StatusRecovering:
		default:
			errs = append(errs, fmt.Errorf("%s: status %q is not ready, engaged, or recovering", name, e.Status))
		}
		if sec, ok := s.Map.sector(e.At.Sector); !ok {
			errs = append(errs, fmt.Errorf("%s: at %s, in a sector that does not exist", name, e.At.Key()))
		} else if !sec.inside(e.At.Point) {
			errs = append(errs, fmt.Errorf("%s: at %s, outside the grid", name, e.At.Key()))
		} else if sec.obstacle(e.At.Point) {
			errs = append(errs, fmt.Errorf("%s: at %s, on an obstacle", name, e.At.Key()))
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
	for _, key := range sortedKeys(s.Progress) {
		p := s.Progress[key]
		if !objectives[key] {
			errs = append(errs, fmt.Errorf("progress on %s: not an objective", key))
		}
		if !s.isFaction(p.Faction) {
			errs = append(errs, fmt.Errorf("progress on %s: faction %q is not in the exercise", key, p.Faction))
		}
		if p.Rounds < 1 || p.Rounds >= CaptureRounds {
			errs = append(errs, fmt.Errorf("progress on %s: %d rounds, want 1 to %d", key, p.Rounds, CaptureRounds-1))
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
	out.Elements = make([]Element, len(s.Elements))
	for i, e := range s.Elements {
		e.Health = append([]int(nil), e.Health...)
		out.Elements[i] = e
	}
	out.Holders = maps.Clone(s.Holders)
	if out.Holders == nil {
		out.Holders = map[string]string{}
	}
	out.Progress = maps.Clone(s.Progress)
	if out.Progress == nil {
		out.Progress = map[string]Progress{}
	}
	return out
}
