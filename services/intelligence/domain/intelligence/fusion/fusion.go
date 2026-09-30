package fusion

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

// String returns the location as "sector:x,y", the key the lists of a
// [Picture] sort their locations by.
func (l Location) String() string {
	return l.Sector + ":" + strconv.Itoa(l.X) + "," + strconv.Itoa(l.Y)
}

// Sector is a sector of the map, as far as intelligence reads it: its ID
// and the size of its grid.
type Sector struct {
	ID     string `json:"id"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// Map is the exercise's public map, as far as intelligence reads it: each
// sector's size. The map holds no objectives; a faction learns one when an
// observation reports it.
type Map struct {
	Sectors []Sector `json:"sectors"`
}

// Element is an element as an observation reports it: its ID, its faction,
// its kind, its strength, the health of each of its living operators, its
// status, and where it stands.
type Element struct {
	ID       string   `json:"id"`
	Faction  string   `json:"faction"`
	Kind     string   `json:"kind"`
	Strength int      `json:"strength"`
	Health   []int    `json:"health"`
	Status   string   `json:"status"`
	At       Location `json:"at"`
}

// clone returns e with a copy of its health, so the copy shares nothing
// with e.
func (e Element) clone() Element {
	e.Health = slices.Clone(e.Health)
	return e
}

// Sight returns the Chebyshev distance an element of kind sees within its
// sector: a squad 1, a scout 2, and an unknown kind none.
func Sight(kind string) int {
	switch kind {
	case "squad":
		return 1
	case "scout":
		return 2
	}
	return 0
}

// sees reports whether e sees at: a cell of its own sector within its
// kind's sight.
func (e Element) sees(at Location) bool {
	if e.At.Sector != at.Sector {
		return false
	}
	return max(abs(e.At.X-at.X), abs(e.At.Y-at.Y)) <= Sight(e.Kind)
}

// ObjectiveStatus is an objective an observation reports, and the faction
// holding it, or "" when it is unheld.
type ObjectiveStatus struct {
	At     Location `json:"at"`
	Holder string   `json:"holder"`
}

// Observation is one faction's observation of one round: its own elements,
// the enemy elements they see, and the objectives in their sight.
type Observation struct {
	Round      int               `json:"round"`
	Own        []Element         `json:"own"`
	Contacts   []Element         `json:"contacts"`
	Objectives []ObjectiveStatus `json:"objectives"`
}

// Contact is an enemy element the faction knows of, as it was last seen:
// the round it was seen in, and its age, the rounds since.
type Contact struct {
	Element
	Seen int `json:"seen"`
	Age  int `json:"age"`
}

// Objective is what the faction knows of one objective, which is listed
// only once one of its elements has seen it. Holder is the faction that
// held it when last seen ("" for none), Seen is that round, and Age the
// rounds since. Known is always true; it stays so the payload's shape does.
type Objective struct {
	At     Location `json:"at"`
	Holder string   `json:"holder"`
	Known  bool     `json:"known"`
	Seen   int      `json:"seen"`
	Age    int      `json:"age"`
}

// Picture is what a faction knows after a round: its own elements, the
// contacts it knows of, the objectives it has seen, and the cells its
// elements have had in sight. Round is the round it is of, -1 before the
// first. Every list is sorted: elements and contacts by ID, objectives by
// location, and explored cells by sector, then y, then x. Grid is the
// map's sectors, kept so the explored cells stay within them; it is not
// part of the picture's JSON.
type Picture struct {
	Round      int         `json:"round"`
	Own        []Element   `json:"own"`
	Contacts   []Contact   `json:"contacts"`
	Objectives []Objective `json:"objectives"`
	Explored   []Location  `json:"explored"`
	Grid       []Sector    `json:"-"`
}

// Open returns the picture a faction starts from over map m: no element,
// no contact, no objective, and nothing explored.
func Open(m Map) Picture {
	return Picture{
		Round:      -1,
		Own:        []Element{},
		Contacts:   []Contact{},
		Objectives: []Objective{},
		Explored:   []Location{},
		Grid:       slices.Clone(m.Sectors),
	}
}

// Fuse returns the picture prev becomes with obs, a later round's
// observation. It takes the observed own elements as they are, and every
// observed contact and objective as seen this round. A contact it does not
// observe keeps its last-seen cell and ages, and drops once its age exceeds
// k or one of the own elements sees its cell. An objective it observes is
// added when it is new; one it does not observe keeps what it was last seen
// as, and ages. An objective an [Alert] recorded from a later round than
// the observation's stands as it is, at age 0. The cells the own elements
// see join the explored ones.
func Fuse(prev Picture, obs Observation, k int) Picture {
	round := obs.Round
	p := Picture{
		Round:      round,
		Own:        make([]Element, 0, len(obs.Own)),
		Contacts:   []Contact{},
		Objectives: []Objective{},
		Grid:       prev.Grid,
	}
	for _, e := range obs.Own {
		p.Own = append(p.Own, e.clone())
	}
	slices.SortFunc(p.Own, func(a, b Element) int { return cmp.Compare(a.ID, b.ID) })

	observed := make(map[string]bool, len(obs.Contacts))
	for _, e := range obs.Contacts {
		observed[e.ID] = true
		p.Contacts = append(p.Contacts, Contact{Element: e.clone(), Seen: round})
	}
	for _, c := range prev.Contacts {
		if observed[c.ID] {
			continue
		}
		c.Element = c.clone()
		c.Age = round - c.Seen
		if c.Age > k || p.sees(c.At) {
			continue
		}
		p.Contacts = append(p.Contacts, c)
	}
	slices.SortFunc(p.Contacts, func(a, b Contact) int { return cmp.Compare(a.ID, b.ID) })

	status := make(map[Location]string, len(obs.Objectives))
	for _, o := range obs.Objectives {
		status[o.At] = o.Holder
	}
	for _, o := range prev.Objectives {
		if holder, ok := status[o.At]; ok {
			delete(status, o.At)
			if o.Seen <= round {
				o = Objective{At: o.At, Holder: holder, Known: true, Seen: round}
			}
		}
		o.Age = max(round-o.Seen, 0)
		p.Objectives = append(p.Objectives, o)
	}
	for at, holder := range status {
		p.Objectives = append(p.Objectives, Objective{At: at, Holder: holder, Known: true, Seen: round})
	}
	slices.SortFunc(p.Objectives, byLocation)

	p.Explored = p.explore(prev.Explored)
	return p
}

// Alert returns the picture p becomes when an alert reports that the
// objective at at was held by holder in round. The objective is set to that
// sighting, added when p does not list it, and aged against p's round, or 0
// when the alert is of that round or a later one. An objective p already
// saw in a later round than the alert's stands as it is. It reports whether
// the result differs from p: an alert that restates what p lists changes
// nothing. The result shares nothing mutable with p.
func Alert(p Picture, at Location, holder string, round int) (Picture, bool) {
	out := p
	out.Own = make([]Element, len(p.Own))
	for i, e := range p.Own {
		out.Own[i] = e.clone()
	}
	out.Contacts = make([]Contact, len(p.Contacts))
	for i, c := range p.Contacts {
		c.Element = c.clone()
		out.Contacts[i] = c
	}
	out.Objectives = slices.Clone(p.Objectives)
	out.Explored = slices.Clone(p.Explored)
	out.Grid = slices.Clone(p.Grid)

	i := slices.IndexFunc(out.Objectives, func(o Objective) bool { return o.At == at })
	if i >= 0 && out.Objectives[i].Seen > round {
		return out, false
	}
	o := Objective{At: at, Holder: holder, Known: true, Seen: round, Age: max(p.Round-round, 0)}
	if i >= 0 {
		changed := out.Objectives[i] != o
		out.Objectives[i] = o
		return out, changed
	}
	out.Objectives = append(out.Objectives, o)
	slices.SortFunc(out.Objectives, byLocation)
	return out, true
}

// explore returns the cells of explored and every cell inside a sector's
// grid that one of the picture's own elements sees, each once.
func (p Picture) explore(explored []Location) []Location {
	size := make(map[string]Sector, len(p.Grid))
	for _, s := range p.Grid {
		size[s.ID] = s
	}
	set := make(map[Location]bool, len(explored))
	for _, at := range explored {
		set[at] = true
	}
	for _, e := range p.Own {
		s, ok := size[e.At.Sector]
		if !ok {
			continue
		}
		r := Sight(e.Kind)
		for y := max(e.At.Y-r, 0); y <= min(e.At.Y+r, s.Height-1); y++ {
			for x := max(e.At.X-r, 0); x <= min(e.At.X+r, s.Width-1); x++ {
				set[Location{Sector: s.ID, X: x, Y: y}] = true
			}
		}
	}
	out := make([]Location, 0, len(set))
	for at := range set {
		out = append(out, at)
	}
	slices.SortFunc(out, func(a, b Location) int {
		return cmp.Or(cmp.Compare(a.Sector, b.Sector), cmp.Compare(a.Y, b.Y), cmp.Compare(a.X, b.X))
	})
	return out
}

// sees reports whether any of the picture's own elements sees at.
func (p Picture) sees(at Location) bool {
	return slices.ContainsFunc(p.Own, func(e Element) bool { return e.sees(at) })
}

func byLocation(a, b Objective) int { return cmp.Compare(a.At.String(), b.At.String()) }

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
