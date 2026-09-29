package route

import (
	"cmp"
	"slices"
	"strconv"
)

// Point is a cell of a sector's grid.
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

// String returns the location as "sector:x,y".
func (l Location) String() string {
	return l.Sector + ":" + strconv.Itoa(l.X) + "," + strconv.Itoa(l.Y)
}

// Gate is a gate's cell in its own sector, and the location of the gate it
// links to.
type Gate struct {
	At Point    `json:"at"`
	To Location `json:"to"`
}

// Sector is one W×H grid of the map, with its features.
type Sector struct {
	ID         string  `json:"id"`
	Width      int     `json:"width"`
	Height     int     `json:"height"`
	Obstacles  []Point `json:"obstacles"`
	Objectives []Point `json:"objectives"`
	Gates      []Gate  `json:"gates"`
}

// Map is the exercise's public map.
type Map struct {
	Sectors []Sector `json:"sectors"`
}

// Objectives returns the location of every objective of m, in the order the
// map lists them.
func (m Map) Objectives() []Location {
	var out []Location
	for _, s := range m.Sectors {
		for _, p := range s.Objectives {
			out = append(out, Location{Sector: s.ID, Point: p})
		}
	}
	return out
}

// Element is one of a faction's elements: its ID, its kind, and where it
// stands.
type Element struct {
	ID   string   `json:"id"`
	Kind string   `json:"kind"`
	At   Location `json:"at"`
}

// Moves returns how many steps an element of kind may take in one round:
// a force 1, a scout 2, and an unknown kind none.
func Moves(kind string) int {
	switch kind {
	case "force":
		return 1
	case "scout":
		return 2
	}
	return 0
}

// Order is one element's steps for a round, the shape exercise records.
type Order struct {
	Element string     `json:"element"`
	Steps   []Location `json:"steps"`
}

// Path returns a shortest sequence of steps from from to to, excluding
// from, or false when to cannot be reached. Among paths of equal length it
// prefers, at each cell, the step up, right, down, left, then the gate's
// traversal, so a plan is deterministic.
func (m Map) Path(from, to Location) ([]Location, bool) {
	if from == to {
		return nil, true
	}
	prev := map[Location]Location{from: from}
	queue := []Location{from}
	for len(queue) > 0 {
		at := queue[0]
		queue = queue[1:]
		for _, next := range m.steps(at) {
			if _, seen := prev[next]; seen {
				continue
			}
			prev[next] = at
			if next == to {
				var path []Location
				for l := to; l != from; l = prev[l] {
					path = append(path, l)
				}
				slices.Reverse(path)
				return path, true
			}
			queue = append(queue, next)
		}
	}
	return nil, false
}

// steps returns the locations one step from at.
func (m Map) steps(at Location) []Location {
	s, ok := m.sector(at.Sector)
	if !ok {
		return nil
	}
	var out []Location
	for _, d := range []Point{{0, -1}, {1, 0}, {0, 1}, {-1, 0}} {
		p := Point{X: at.X + d.X, Y: at.Y + d.Y}
		if s.open(p) {
			out = append(out, Location{Sector: s.ID, Point: p})
		}
	}
	for _, g := range s.Gates {
		if g.At == at.Point {
			if to, ok := m.sector(g.To.Sector); ok && to.open(g.To.Point) {
				out = append(out, g.To)
			}
		}
	}
	return out
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

// open reports whether p lies inside s's grid and is not an obstacle.
func (s Sector) open(p Point) bool {
	return p.X >= 0 && p.X < s.Width && p.Y >= 0 && p.Y < s.Height && !slices.Contains(s.Obstacles, p)
}

// Plan returns the orders that carry a faction's elements toward their
// targets this round: each element with a target it does not stand on and
// can reach takes up to its kind's moves along a shortest path.
//
// No two of the faction's elements may end on one cell, the rule under
// which exercise refuses an order whole, so Plan applies exercise's own
// test to the planned ends: an element that moves, and ends where another
// element ends, is refused. Where movers alone share a cell, the one with
// the lowest ID keeps it; where an element that stays is there, none does.
// Plan shortens each refused element's steps by one and tests again, until
// no end is shared, so an element can follow another into the cell it
// leaves, and two can swap cells. An element that ends up with no steps
// holds, and gets no order.
func Plan(m Map, elements []Element, targets map[string]Location) []Order {
	es := slices.Clone(elements)
	slices.SortFunc(es, func(a, b Element) int { return cmp.Compare(a.ID, b.ID) })
	paths := make([][]Location, len(es))
	for i, e := range es {
		target, ok := targets[e.ID]
		if !ok {
			continue
		}
		if path, ok := m.Path(e.At, target); ok {
			paths[i] = path[:min(Moves(e.Kind), len(path))]
		}
	}
	end := func(i int) Location {
		if n := len(paths[i]); n > 0 {
			return paths[i][n-1]
		}
		return es[i].At
	}
	for refused := true; refused; {
		refused = false
		at := make(map[Location][]int, len(es))
		for i := range es {
			at[end(i)] = append(at[end(i)], i)
		}
		for _, group := range at {
			if len(group) < 2 {
				continue
			}
			// group is in ID order. The lowest-ID mover keeps the cell unless
			// an element that stays is there.
			keep := -1
			if !slices.ContainsFunc(group, func(i int) bool { return len(paths[i]) == 0 }) {
				keep = group[0]
			}
			for _, i := range group {
				if i != keep && len(paths[i]) > 0 {
					paths[i] = paths[i][:len(paths[i])-1]
					refused = true
				}
			}
		}
	}
	var orders []Order
	for i, e := range es {
		if len(paths[i]) > 0 {
			orders = append(orders, Order{Element: e.ID, Steps: slices.Clip(paths[i])})
		}
	}
	return orders
}
