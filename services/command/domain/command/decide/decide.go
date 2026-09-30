package decide

import (
	"cmp"
	"math"
	"slices"
	"strconv"
)

// EngageRange is the most steps a squad goes to engage a contact.
const EngageRange = 3

// ReinforceRange is the most steps a squad goes to reinforce a fight.
const ReinforceRange = 4

// SpreadRange is the Chebyshev distance within which a search target
// crowds another element's: an element prefers a cell farther than this
// from every search target already claimed.
const SpreadRange = 2

// The kinds of element.
const (
	// Squad is an element of up to four operators; it engages and
	// reinforces.
	Squad = "squad"
	// Scout is an element of one operator; it never seeks a fight.
	Scout = "scout"
)

// The statuses of an element.
const (
	// Ready is an element free to take any order.
	Ready = "ready"
	// Engaged is an element whose cell holds the enemy: it is pinned there,
	// and only a one-step retreat moves it.
	Engaged = "engaged"
	// Recovering is an element that retreated last round; the exercise
	// refuses its next order.
	Recovering = "recovering"
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

// Element is one of the faction's own elements, as an assessment reports
// it: its ID, its kind, its strength, the sum of its operators' health, its
// status, and where it stands.
type Element struct {
	ID       string   `json:"id"`
	Kind     string   `json:"kind"`
	Strength int      `json:"strength"`
	Health   []int    `json:"health"`
	Status   string   `json:"status"`
	At       Location `json:"at"`
}

// Contact is an enemy element the assessment knows of: its ID, kind,
// strength and status, the cell it was last seen in, and how many rounds
// ago it was seen there, 0 for this round.
type Contact struct {
	ID       string   `json:"id"`
	Kind     string   `json:"kind"`
	Strength int      `json:"strength"`
	Status   string   `json:"status"`
	At       Location `json:"at"`
	Age      int      `json:"age"`
}

// Objective is what the assessment knows of one objective the faction has
// discovered. Holder is the faction that held it when last seen, and means
// nothing unless Known.
type Objective struct {
	At     Location `json:"at"`
	Holder string   `json:"holder"`
	Known  bool     `json:"known"`
}

// Assessment is what a faction knows after a round, as far as command reads
// it: the round, the faction's own elements, its contacts, the objectives
// it has discovered, and every cell its elements have ever had in sight.
type Assessment struct {
	Round      int         `json:"round"`
	Own        []Element   `json:"own"`
	Contacts   []Contact   `json:"contacts"`
	Objectives []Objective `json:"objectives"`
	Explored   []Location  `json:"explored"`
}

// Rule is the rule a decision was made by.
type Rule string

// The rules, in the order [Decide] tries them. Engage is tried twice: an
// engaged element that does not retreat and is outmatched holds its fight
// by it, beside pursue, before any reinforce, and a free squad seeks a
// weaker contact by it, after.
const (
	// Retreat steps an engaged element out of a fight it is losing, or a
	// scout out of any fight, into an open neighboring cell.
	Retreat Rule = "retreat"
	// Pursue keeps an engaged element in a fight its faction at least
	// matches, ready to fire on an enemy that retreats from it.
	Pursue Rule = "pursue"
	// Engage keeps an outmatched engaged element in its fight, or heads a
	// squad for a weaker contact within reach.
	Engage Rule = "engage"
	// Reinforce heads a free squad for a fight of its faction within reach.
	Reinforce Rule = "reinforce"
	// Secure heads an element for a known objective its faction does not
	// hold.
	Secure Rule = "secure"
	// Search heads an element for an unexplored cell.
	Search Rule = "search"
	// Hold keeps an element where it stands.
	Hold Rule = "hold"
)

// Decision is what command directs one element to do: the rule, the
// contact a retreat leaves, a pursue or an engage fights, or a reinforce
// heads for, and the target, nil for a hold.
type Decision struct {
	Element string    `json:"element"`
	Rule    Rule      `json:"rule"`
	Contact string    `json:"contact,omitempty"`
	Target  *Location `json:"target"`
}

// Decide returns a decision for each of the assessment's own elements, in
// ID order, for faction. standing holds the decisions made on an earlier
// assessment: an element that was securing an objective or searching a
// cell keeps it while it is still one to secure or search and the element
// can reach it.
func Decide(m Map, faction string, a Assessment, standing []Decision) []Decision {
	own := slices.Clone(a.Own)
	slices.SortFunc(own, func(x, y Element) int { return cmp.Compare(x.ID, y.ID) })

	open := make(map[Location]bool, len(a.Objectives))
	for _, o := range a.Objectives {
		// known is tested first: an unknown objective's holder is empty,
		// and it is unheld whatever that field says.
		open[o.At] = !o.Known || o.Holder != faction
	}
	// A cell an element stands in is in its sight, whether or not the
	// assessment lists it.
	explored := make(map[Location]bool, len(a.Explored)+len(own))
	for _, l := range a.Explored {
		explored[l] = true
	}
	was := make(map[string]Decision, len(standing))
	for _, d := range standing {
		was[d.Element] = d
	}

	// The strength each side has in a cell: the faction's from all its
	// elements there, the enemy's from the contacts seen there this round.
	ours, theirs := map[Location]int{}, map[Location]int{}
	for _, e := range own {
		ours[e.At] += e.Strength
		explored[e.At] = true
	}
	for _, c := range a.Contacts {
		if c.Age == 0 {
			theirs[c.At] += c.Strength
		}
	}

	decisions := make([]Decision, len(own))
	dist := make([]map[Location]int, len(own))
	fights := map[Location]bool{}
	for i, e := range own {
		dist[i] = m.distances(e.At)
		enemy, ok := strongest(a.Contacts, e.At)
		if e.Status != Engaged || !ok {
			continue
		}
		if e.Kind == Scout || 3*ours[e.At] < 2*theirs[e.At] {
			if to, ok := m.retreat(e.At, a.Contacts); ok {
				decisions[i] = Decision{Element: e.ID, Rule: Retreat, Contact: enemy.ID, Target: &to}
				continue
			}
		}
		at, rule := e.At, Engage
		if ours[at] >= theirs[at] {
			rule = Pursue
		}
		fights[at] = true
		decisions[i] = Decision{Element: e.ID, Rule: rule, Contact: enemy.ID, Target: &at}
	}
	for i, e := range own {
		if decisions[i].Rule != "" {
			continue
		}
		decisions[i] = reinforce(e, fights, a.Contacts, dist[i])
		if decisions[i].Rule == "" {
			decisions[i] = engage(e, a.Contacts, dist[i])
		}
	}

	// A scout searches while any unexplored cell is in its reach, and
	// secures only once none is.
	unexplored := func(l Location) bool { return !explored[l] }
	searches := make([]bool, len(own))
	for i, e := range own {
		if e.Kind == Scout {
			_, searches[i] = nearest(dist[i], unexplored)
		}
	}

	// Standing targets are claimed first, in ID order, so an element that
	// was securing an objective keeps it.
	claimed := map[Location]bool{}
	for i, e := range own {
		d, ok := was[e.ID]
		if decisions[i].Rule != "" || searches[i] || !ok || d.Rule != Secure || d.Target == nil {
			continue
		}
		at := *d.Target
		if _, reach := dist[i][at]; open[at] && reach && !claimed[at] {
			claimed[at] = true
			decisions[i] = Decision{Element: e.ID, Rule: Secure, Target: &at}
		}
	}
	for i, e := range own {
		if decisions[i].Rule != "" || searches[i] {
			continue
		}
		if at, ok := nearest(dist[i], func(l Location) bool { return open[l] && !claimed[l] }); ok {
			claimed[at] = true
			decisions[i] = Decision{Element: e.ID, Rule: Secure, Target: &at}
		}
	}

	// Scouts search before squads. Within each kind, standing search
	// targets are claimed first, in ID order, then each element picks the
	// nearest unexplored cell unclaimed, preferring one clear of every
	// search target claimed so far, so the searchers spread.
	var targets []Location
	sought := map[Location]bool{}
	for _, kind := range []string{Scout, Squad} {
		for i, e := range own {
			d, ok := was[e.ID]
			if decisions[i].Rule != "" || e.Kind != kind || !ok || d.Rule != Search || d.Target == nil {
				continue
			}
			at := *d.Target
			if _, reach := dist[i][at]; reach && !explored[at] && !sought[at] {
				sought[at] = true
				targets = append(targets, at)
				decisions[i] = Decision{Element: e.ID, Rule: Search, Target: &at}
			}
		}
		for i, e := range own {
			if decisions[i].Rule != "" || e.Kind != kind {
				continue
			}
			free := func(l Location) bool { return !explored[l] && !sought[l] }
			at, ok := nearest(dist[i], func(l Location) bool { return free(l) && apart(l, targets) })
			if !ok {
				at, ok = nearest(dist[i], free)
			}
			if ok {
				sought[at] = true
				targets = append(targets, at)
				decisions[i] = Decision{Element: e.ID, Rule: Search, Target: &at}
			}
		}
	}

	for i, e := range own {
		if decisions[i].Rule == "" {
			decisions[i] = Decision{Element: e.ID, Rule: Hold}
		}
	}
	return decisions
}

// apart reports whether l lies farther than [SpreadRange], by Chebyshev
// distance, from every one of targets in its sector.
func apart(l Location, targets []Location) bool {
	for _, t := range targets {
		if t.Sector == l.Sector && max(abs(t.X-l.X), abs(t.Y-l.Y)) <= SpreadRange {
			return false
		}
	}
	return true
}

// strongest returns the strongest contact seen this round at at, ties going
// to the lowest ID, or false when none is.
func strongest(contacts []Contact, at Location) (Contact, bool) {
	var best Contact
	found := false
	for _, c := range contacts {
		if c.Age != 0 || c.At != at {
			continue
		}
		if !found || c.Strength > best.Strength || c.Strength == best.Strength && c.ID < best.ID {
			best, found = c, true
		}
	}
	return best, found
}

// retreat returns the cell an element at at retreats to: the orthogonally
// adjacent open cell of its sector, holding no contact seen this round,
// farthest by Chebyshev distance from the nearest known contact outside
// the fight at at, ties going to the first of up, right, down, left. The
// fight's own contacts are one step from every neighbor, so they would
// tie them all. It returns false when no such neighbor is open.
func (m Map) retreat(at Location, contacts []Contact) (Location, bool) {
	s, ok := m.sector(at.Sector)
	if !ok {
		return Location{}, false
	}
	var best Location
	bestGap, found := -1, false
	for _, d := range directions {
		p := Point{X: at.X + d.X, Y: at.Y + d.Y}
		if !s.open(p) {
			continue
		}
		to := Location{Sector: s.ID, Point: p}
		gap, held := math.MaxInt, false
		for _, c := range contacts {
			if c.At.Sector != s.ID || c.At == at {
				continue
			}
			if c.At == to && c.Age == 0 {
				held = true
				break
			}
			gap = min(gap, max(abs(c.At.X-p.X), abs(c.At.Y-p.Y)))
		}
		if !held && gap > bestGap {
			best, bestGap, found = to, gap, true
		}
	}
	return best, found
}

// reinforce returns the reinforce decision for e, a squad free to move,
// toward the nearest of fights within [ReinforceRange] steps, ties going to
// the lowest by [Location.String], or the zero Decision when there is none.
// Its contact is the strongest enemy in that fight.
func reinforce(e Element, fights map[Location]bool, contacts []Contact, dist map[Location]int) Decision {
	if e.Kind != Squad || e.Status != Ready && e.Status != Recovering {
		return Decision{}
	}
	at, ok := nearest(dist, func(l Location) bool { return fights[l] && dist[l] <= ReinforceRange })
	if !ok {
		return Decision{}
	}
	enemy, _ := strongest(contacts, at)
	return Decision{Element: e.ID, Rule: Reinforce, Contact: enemy.ID, Target: &at}
}

// engage returns the engage decision for e, a squad, against the nearest
// contact weaker than itself within [EngageRange] steps, ties going to the
// lowest ID, or the zero Decision when there is none.
func engage(e Element, contacts []Contact, dist map[Location]int) Decision {
	if e.Kind != Squad {
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
	for _, d := range directions {
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

// directions are the four orthogonal steps, in the order up, right, down,
// left.
var directions = []Point{{0, -1}, {1, 0}, {0, 1}, {-1, 0}}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
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
