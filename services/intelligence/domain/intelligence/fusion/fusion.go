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

// Map is the exercise's public map, as far as intelligence reads it: each
// sector's objectives.
type Map struct {
	Sectors []struct {
		ID         string  `json:"id"`
		Objectives []Point `json:"objectives"`
	} `json:"sectors"`
}

// Objectives returns the location of every objective of m.
func (m Map) Objectives() []Location {
	var out []Location
	for _, s := range m.Sectors {
		for _, p := range s.Objectives {
			out = append(out, Location{Sector: s.ID, Point: p})
		}
	}
	return out
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

// Objective is what the faction knows of one objective. Known reports
// whether any of its elements has seen it; when it has, Holder is the
// faction that held it when last seen ("" for none), Seen is that round,
// and Age the rounds since.
type Objective struct {
	At     Location `json:"at"`
	Holder string   `json:"holder"`
	Known  bool     `json:"known"`
	Seen   int      `json:"seen"`
	Age    int      `json:"age"`
}

// Picture is what a faction knows after a round: its own elements, the
// contacts it knows of, and every objective of the map. Round is the round
// it is of, -1 before the first. Every list is sorted: elements and
// contacts by ID, and objectives by location.
type Picture struct {
	Round      int         `json:"round"`
	Own        []Element   `json:"own"`
	Contacts   []Contact   `json:"contacts"`
	Objectives []Objective `json:"objectives"`
}

// Open returns the picture a faction starts from: the map's objectives,
// none of them known, and no element.
func Open(objectives []Location) Picture {
	p := Picture{Round: -1, Own: []Element{}, Contacts: []Contact{}, Objectives: []Objective{}}
	for _, at := range objectives {
		p.Objectives = append(p.Objectives, Objective{At: at, Seen: -1})
	}
	slices.SortFunc(p.Objectives, byLocation)
	return p
}

// Fuse returns the picture prev becomes with obs, a later round's
// observation. It takes the observed own elements as they are, and every
// observed contact and objective as seen this round. A contact it does not
// observe keeps its last-seen cell and ages, and drops once its age exceeds
// k or one of the own elements sees its cell. An objective it does not
// observe keeps what it was last seen as, and ages once known.
func Fuse(prev Picture, obs Observation, k int) Picture {
	round := obs.Round
	p := Picture{
		Round:      round,
		Own:        make([]Element, 0, len(obs.Own)),
		Contacts:   []Contact{},
		Objectives: []Objective{},
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
			o = Objective{At: o.At, Holder: holder, Known: true, Seen: round}
		} else if o.Known {
			o.Age = round - o.Seen
		}
		p.Objectives = append(p.Objectives, o)
	}
	slices.SortFunc(p.Objectives, byLocation)
	return p
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
