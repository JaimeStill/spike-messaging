package rules

import (
	"errors"
	"fmt"
	"strconv"
)

// Kind is the kind of an element, which fixes how far it moves and sees,
// and how many operators it fields.
type Kind string

// The kinds of element.
const (
	// Squad is a fighting element of up to four operators. It makes one
	// move per round and sees one cell.
	Squad Kind = "squad"
	// Scout is a reconnaissance element of one operator. It makes two moves
	// per round and sees two cells.
	Scout Kind = "scout"
)

// Moves returns the number of steps an element of kind k may take in one
// round, or 0 for a kind the package does not know.
func (k Kind) Moves() int {
	switch k {
	case Squad:
		return 1
	case Scout:
		return 2
	}
	return 0
}

// Sight returns the Chebyshev distance an element of kind k sees within its
// sector, or 0 for a kind the package does not know.
func (k Kind) Sight() int {
	switch k {
	case Squad:
		return 1
	case Scout:
		return 2
	}
	return 0
}

// Operators returns the number of operators an element of kind k fields at
// full strength, or 0 for a kind the package does not know.
func (k Kind) Operators() int {
	switch k {
	case Squad:
		return 4
	case Scout:
		return 1
	}
	return 0
}

// Point is a cell of a sector's grid: X in [0, Width) and Y in [0, Height).
type Point struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// Location is a cell of the map: a point in a named sector. Its JSON form
// flattens the point beside the sector.
type Location struct {
	Sector string `json:"sector"`
	Point
}

// Key returns the location as "sector:x,y", a string that is stable and
// unique to the location, used to key the objectives a [State] holds.
func (l Location) Key() string {
	return l.Sector + ":" + strconv.Itoa(l.X) + "," + strconv.Itoa(l.Y)
}

// Gate is a gate's cell in its own sector, and the location of the gate it
// links to in another sector. Links are reciprocal.
type Gate struct {
	At Point    `json:"at"`
	To Location `json:"to"`
}

// Sector is one W×H grid of the map, with its features. A cell carries at
// most one feature: an obstacle, which is impassable, an objective, or a
// gate.
type Sector struct {
	ID         string  `json:"id"`
	Width      int     `json:"width"`
	Height     int     `json:"height"`
	Obstacles  []Point `json:"obstacles"`
	Objectives []Point `json:"objectives"`
	Gates      []Gate  `json:"gates"`
}

// Map is the exercise's sectors and their features. The map is public:
// every faction knows it.
type Map struct {
	Sectors []Sector `json:"sectors"`
}

// Validate reports every way m breaks the map's rules: sector IDs are
// unique and non-empty; each grid is at least 1×1; every feature lies inside
// its grid; a cell carries at most one feature; every gate links to a gate
// cell inside a different sector, which links back to it; and the map has
// at least one objective.
func (m Map) Validate() error {
	var errs []error
	ids := make(map[string]bool, len(m.Sectors))
	for i, s := range m.Sectors {
		switch {
		case s.ID == "":
			errs = append(errs, fmt.Errorf("sector %d: an empty ID", i))
		case ids[s.ID]:
			errs = append(errs, fmt.Errorf("sector %q: a duplicate ID", s.ID))
		}
		ids[s.ID] = true
	}
	objectives := 0
	for _, s := range m.Sectors {
		if s.Width < 1 || s.Height < 1 {
			errs = append(errs, fmt.Errorf("sector %q: a %d×%d grid, want at least 1×1", s.ID, s.Width, s.Height))
		}
		seen := make(map[Point]string)
		feature := func(what string, p Point) {
			if !s.inside(p) {
				errs = append(errs, fmt.Errorf("sector %q: %s at %d,%d lies outside the grid", s.ID, what, p.X, p.Y))
			}
			if prev, ok := seen[p]; ok {
				errs = append(errs, fmt.Errorf("sector %q: %s at %d,%d shares its cell with %s", s.ID, what, p.X, p.Y, prev))
				return
			}
			seen[p] = what
		}
		for _, p := range s.Obstacles {
			feature("an obstacle", p)
		}
		for _, p := range s.Objectives {
			feature("an objective", p)
			objectives++
		}
		for _, g := range s.Gates {
			feature("a gate", g.At)
			if err := m.checkLink(s, g); err != nil {
				errs = append(errs, err)
			}
		}
	}
	if objectives == 0 {
		errs = append(errs, errors.New("no objective"))
	}
	if len(errs) > 0 {
		return fmt.Errorf("rules: map: %w", errors.Join(errs...))
	}
	return nil
}

// checkLink reports whether gate g of sector s links to a gate cell of
// another sector that links back to g.
func (m Map) checkLink(s Sector, g Gate) error {
	at := fmt.Sprintf("sector %q: the gate at %d,%d", s.ID, g.At.X, g.At.Y)
	if g.To.Sector == s.ID {
		return fmt.Errorf("%s links to its own sector", at)
	}
	to, ok := m.sector(g.To.Sector)
	if !ok {
		return fmt.Errorf("%s links to sector %q, which does not exist", at, g.To.Sector)
	}
	if !to.inside(g.To.Point) {
		return fmt.Errorf("%s links to %s, outside its grid", at, g.To.Key())
	}
	back, ok := to.gate(g.To.Point)
	if !ok {
		return fmt.Errorf("%s links to %s, which is not a gate", at, g.To.Key())
	}
	if back.To != (Location{Sector: s.ID, Point: g.At}) {
		return fmt.Errorf("%s links to %s, which links to %s instead", at, g.To.Key(), back.To.Key())
	}
	return nil
}

// sector returns the sector of m with ID id.
func (m Map) sector(id string) (Sector, bool) {
	for _, s := range m.Sectors {
		if s.ID == id {
			return s, true
		}
	}
	return Sector{}, false
}

// objectives returns the location of every objective of m.
func (m Map) objectives() []Location {
	var out []Location
	for _, s := range m.Sectors {
		for _, p := range s.Objectives {
			out = append(out, Location{Sector: s.ID, Point: p})
		}
	}
	return out
}

// clone returns a copy of m that shares no slice with it.
func (m Map) clone() Map {
	out := Map{Sectors: make([]Sector, len(m.Sectors))}
	for i, s := range m.Sectors {
		s.Obstacles = append([]Point(nil), s.Obstacles...)
		s.Objectives = append([]Point(nil), s.Objectives...)
		s.Gates = append([]Gate(nil), s.Gates...)
		out.Sectors[i] = s
	}
	return out
}

// inside reports whether p lies on s's grid.
func (s Sector) inside(p Point) bool {
	return p.X >= 0 && p.X < s.Width && p.Y >= 0 && p.Y < s.Height
}

// obstacle reports whether p carries an obstacle.
func (s Sector) obstacle(p Point) bool {
	for _, o := range s.Obstacles {
		if o == p {
			return true
		}
	}
	return false
}

// gate returns the gate at p, if p carries one.
func (s Sector) gate(p Point) (Gate, bool) {
	for _, g := range s.Gates {
		if g.At == p {
			return g, true
		}
	}
	return Gate{}, false
}
