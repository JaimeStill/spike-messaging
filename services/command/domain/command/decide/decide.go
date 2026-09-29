package decide

import (
	"cmp"
	"slices"
	"strconv"
)

// EngageRange is the most steps a force goes to engage a contact.
const EngageRange = 3

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

// Element is one of the faction's own elements, as an assessment reports
// it: its ID, its kind, its strength, and where it stands.
type Element struct {
	ID       string   `json:"id"`
	Kind     string   `json:"kind"`
	Strength int      `json:"strength"`
	At       Location `json:"at"`
}

// Contact is an enemy element the assessment knows of: its ID, its
// strength, and the cell it was last seen in.
type Contact struct {
	ID       string   `json:"id"`
	Strength int      `json:"strength"`
	At       Location `json:"at"`
}

// Objective is what the assessment knows of one objective. Holder is the
// faction that held it when last seen, and means nothing unless Known.
type Objective struct {
	At     Location `json:"at"`
	Holder string   `json:"holder"`
	Known  bool     `json:"known"`
}

// Assessment is what a faction knows after a round, as far as command reads
// it: the round, the faction's own elements, its contacts, and every
// objective of the map.
type Assessment struct {
	Round      int         `json:"round"`
	Own        []Element   `json:"own"`
	Contacts   []Contact   `json:"contacts"`
	Objectives []Objective `json:"objectives"`
}

// Rule is the rule a decision was made by.
type Rule string

// The rules, in the order [Decide] tries them.
const (
	// Engage heads a force for a weaker contact within reach.
	Engage Rule = "engage"
	// Secure heads an element for an objective its faction does not hold.
	Secure Rule = "secure"
	// Hold keeps an element where it stands.
	Hold Rule = "hold"
)

// Decision is what command directs one element to do: the rule, the
// contact an engage heads for, and the target, nil for a hold.
type Decision struct {
	Element string    `json:"element"`
	Rule    Rule      `json:"rule"`
	Contact string    `json:"contact,omitempty"`
	Target  *Location `json:"target"`
}

// Decide returns a decision for each of the assessment's own elements, in
// ID order, for faction. standing holds the decisions made on an earlier
// assessment: an element that was securing an objective keeps it while the
// objective is still one to secure and the element can reach it.
func Decide(m Map, faction string, a Assessment, standing []Decision) []Decision {
	own := slices.Clone(a.Own)
	slices.SortFunc(own, func(x, y Element) int { return cmp.Compare(x.ID, y.ID) })

	open := make(map[Location]bool, len(a.Objectives))
	for _, o := range a.Objectives {
		// known is tested first: an unknown objective's holder is empty,
		// and it is unheld whatever that field says.
		open[o.At] = !o.Known || o.Holder != faction
	}
	was := make(map[string]Decision, len(standing))
	for _, d := range standing {
		was[d.Element] = d
	}

	decisions := make([]Decision, len(own))
	dist := make([]map[Location]int, len(own))
	for i, e := range own {
		dist[i] = m.distances(e.At)
		decisions[i] = engage(e, a.Contacts, dist[i])
	}

	// Standing targets are claimed first, in ID order, so an element that
	// was securing an objective keeps it.
	claimed := map[Location]bool{}
	for i, e := range own {
		d, ok := was[e.ID]
		if decisions[i].Rule != "" || !ok || d.Rule != Secure || d.Target == nil {
			continue
		}
		at := *d.Target
		if _, reach := dist[i][at]; open[at] && reach && !claimed[at] {
			claimed[at] = true
			decisions[i] = Decision{Element: e.ID, Rule: Secure, Target: &at}
		}
	}
	for i, e := range own {
		if decisions[i].Rule != "" {
			continue
		}
		decisions[i] = Decision{Element: e.ID, Rule: Hold}
		if at, ok := nearest(dist[i], func(l Location) bool { return open[l] && !claimed[l] }); ok {
			claimed[at] = true
			decisions[i] = Decision{Element: e.ID, Rule: Secure, Target: &at}
		}
	}
	return decisions
}

// engage returns the engage decision for e, a force, against the nearest
// weaker contact within [EngageRange] steps, ties going to the lowest ID,
// or the zero Decision when there is none.
func engage(e Element, contacts []Contact, dist map[Location]int) Decision {
	if e.Kind != "force" {
		return Decision{}
	}
	var best *Contact
	for _, c := range contacts {
		n, ok := dist[c.At]
		if !ok || n > EngageRange || c.Strength >= e.Strength {
			continue
		}
		if best == nil || n < dist[best.At] || n == dist[best.At] && c.ID < best.ID {
			best = &c
		}
	}
	if best == nil {
		return Decision{}
	}
	at := best.At
	return Decision{Element: e.ID, Rule: Engage, Contact: best.ID, Target: &at}
}

// nearest returns the nearest location in dist that ok accepts, ties going
// to the lowest by [Location.String], or false when none is.
func nearest(dist map[Location]int, ok func(Location) bool) (Location, bool) {
	var best Location
	found := false
	for l, n := range dist {
		if !ok(l) {
			continue
		}
		if !found || n < dist[best] || n == dist[best] && l.String() < best.String() {
			best, found = l, true
		}
	}
	return best, found
}

// distances returns the path distance from from to every location it
// reaches, from included at 0.
func (m Map) distances(from Location) map[Location]int {
	dist := map[Location]int{from: 0}
	queue := []Location{from}
	for len(queue) > 0 {
		at := queue[0]
		queue = queue[1:]
		for _, next := range m.steps(at) {
			if _, seen := dist[next]; !seen {
				dist[next] = dist[at] + 1
				queue = append(queue, next)
			}
		}
	}
	return dist
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
