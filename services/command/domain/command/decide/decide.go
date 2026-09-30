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
	ID        string  `json:"id"`
	Width     int     `json:"width"`
	Height    int     `json:"height"`
	Obstacles []Point `json:"obstacles"`
	Gates     []Gate  `json:"gates"`
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
// discovered. Holder is the faction that held it when last seen, empty when
// none did. Age is how many rounds ago it was last seen, 0 for this round.
type Objective struct {
	At     Location `json:"at"`
	Holder string   `json:"holder"`
	Age    int      `json:"age"`
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
	// Secure heads an element for a discovered objective its faction does
	// not hold.
	Secure Rule = "secure"
	// Search heads an element for an unexplored cell.
	Search Rule = "search"
	// Rescout heads an element with nothing else to do for the discovered
	// objective its faction does not hold that was seen longest ago, so a
	// stale belief is refreshed.
	Rescout Rule = "rescout"
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
// assessment: an element that was securing or rescouting an objective or
// searching a cell keeps it while it is still one to secure, rescout, or
// search and the element can reach it.
func Decide(m Map, faction string, a Assessment, standing []Decision) []Decision {
	p := newPlan(m, faction, a, standing)
	fights := p.fight(m, a.Contacts)
	for i, e := range p.own {
		if p.decisions[i].Rule != "" {
			continue
		}
		p.decisions[i] = reinforce(e, fights, a.Contacts, p.dist[i])
		if p.decisions[i].Rule == "" {
			p.decisions[i] = engage(e, a.Contacts, p.dist[i])
		}
	}

	// A scout searches while any unexplored cell is in its reach, and
	// secures only once none is.
	unexplored := func(l Location) bool { return !p.explored[l] }
	searches := make([]bool, len(p.own))
	for i, e := range p.own {
		if e.Kind == Scout {
			_, searches[i] = nearest(p.dist[i], unexplored)
		}
	}

	// An element standing on an objective to secure claims it first, so no
	// element nearer the objective in ID order sends it away. Standing
	// targets are claimed next, in ID order, so an element that was
	// securing an objective keeps it.
	claimed := map[Location]bool{}
	for i, e := range p.own {
		if p.decisions[i].Rule != "" || searches[i] || !p.open[e.At] || claimed[e.At] {
			continue
		}
		at := e.At
		claimed[at] = true
		p.decisions[i] = Decision{Element: e.ID, Rule: Secure, Target: &at}
	}
	p.keep(Secure, claimed,
		func(i int) bool { return !searches[i] },
		func(_ Element, at Location) bool { return p.open[at] })
	for i, e := range p.own {
		if p.decisions[i].Rule != "" || searches[i] {
			continue
		}
		if at, ok := nearest(p.dist[i], func(l Location) bool { return p.open[l] && !claimed[l] }); ok {
			claimed[at] = true
			p.decisions[i] = Decision{Element: e.ID, Rule: Secure, Target: &at}
		}
	}

	// Scouts search before squads. Within each kind, standing search
	// targets are claimed first, in ID order, then each element picks the
	// nearest unexplored cell unclaimed, preferring one clear of every
	// search target claimed so far, so the searchers spread.
	var targets []Location
	sought := map[Location]bool{}
	for _, kind := range []string{Scout, Squad} {
		targets = append(targets, p.keep(Search, sought,
			func(i int) bool { return p.own[i].Kind == kind },
			func(_ Element, at Location) bool { return !p.explored[at] })...)
		for i, e := range p.own {
			if p.decisions[i].Rule != "" || e.Kind != kind {
				continue
			}
			free := func(l Location) bool { return !p.explored[l] && !sought[l] }
			at, ok := nearest(p.dist[i], func(l Location) bool { return free(l) && apart(l, targets) })
			if !ok {
				at, ok = nearest(p.dist[i], free)
			}
			if ok {
				sought[at] = true
				targets = append(targets, at)
				p.decisions[i] = Decision{Element: e.ID, Rule: Search, Target: &at}
			}
		}
	}

	// An element with nothing else to do rescouts an objective not held,
	// though another element secures it. Standing rescout targets are
	// claimed first, in ID order, then each element picks the stalest
	// objective no other rescout has claimed. None rescouts the objective
	// it stands on.
	rescouted := map[Location]bool{}
	p.keep(Rescout, rescouted,
		func(int) bool { return true },
		func(e Element, at Location) bool { return p.open[at] && at != e.At })
	for i, e := range p.own {
		if p.decisions[i].Rule != "" {
			continue
		}
		if at, ok := stalest(a.Objectives, p.dist[i], func(l Location) bool {
			return p.open[l] && l != e.At && !rescouted[l]
		}); ok {
			rescouted[at] = true
			p.decisions[i] = Decision{Element: e.ID, Rule: Rescout, Target: &at}
		}
	}

	for i, e := range p.own {
		if p.decisions[i].Rule == "" {
			p.decisions[i] = Decision{Element: e.ID, Rule: Hold}
		}
	}
	return p.decisions
}

// plan is what [Decide] works from and fills in: the faction's elements in
// ID order, the path distance from each to every location it reaches, the
// decision each has so far, and what the assessment and the standing
// decisions say of the map's cells.
type plan struct {
	own       []Element
	dist      []map[Location]int
	decisions []Decision

	// open marks each discovered objective the faction does not hold, one
	// to secure or rescout; explored each cell its elements have had in
	// sight; and occupied each cell one of them stands in.
	open     map[Location]bool
	explored map[Location]bool
	occupied map[Location]bool
	// ours and theirs are the strength each side has in a cell: the
	// faction's from all its elements there, the enemy's from the contacts
	// seen there this round.
	ours, theirs map[Location]int
	// was is the standing decision of each element that has one, by ID.
	was map[string]Decision
}

// newPlan returns the plan for faction's elements on assessment a, with no
// element decided yet.
func newPlan(m Map, faction string, a Assessment, standing []Decision) *plan {
	own := slices.Clone(a.Own)
	slices.SortFunc(own, func(x, y Element) int { return cmp.Compare(x.ID, y.ID) })
	p := &plan{
		own:       own,
		dist:      make([]map[Location]int, len(own)),
		decisions: make([]Decision, len(own)),
		open:      make(map[Location]bool, len(a.Objectives)),
		explored:  make(map[Location]bool, len(a.Explored)+len(own)),
		occupied:  map[Location]bool{},
		ours:      map[Location]int{},
		theirs:    map[Location]int{},
		was:       make(map[string]Decision, len(standing)),
	}
	for _, o := range a.Objectives {
		p.open[o.At] = o.Holder != faction
	}
	for _, l := range a.Explored {
		p.explored[l] = true
	}
	for _, d := range standing {
		p.was[d.Element] = d
	}
	// A cell an element stands in is in its sight, whether or not the
	// assessment lists it.
	for i, e := range own {
		p.dist[i] = m.distances(e.At)
		p.ours[e.At] += e.Strength
		p.occupied[e.At] = true
		p.explored[e.At] = true
	}
	for _, c := range a.Contacts {
		if c.Age == 0 {
			p.theirs[c.At] += c.Strength
		}
	}
	return p
}

// fight decides each engaged element's fight, in ID order: it retreats,
// or holds its cell by pursue or engage. It returns the cells held that
// way, the fights a free squad may reinforce.
func (p *plan) fight(m Map, contacts []Contact) map[Location]bool {
	fights := map[Location]bool{}
	for i, e := range p.own {
		enemy, ok := strongest(contacts, e.At)
		if e.Status != Engaged || !ok {
			continue
		}
		if e.Kind == Scout || 3*p.ours[e.At] < 2*p.theirs[e.At] {
			if to, ok := m.retreat(e.At, contacts, p.occupied); ok {
				p.decisions[i] = Decision{Element: e.ID, Rule: Retreat, Contact: enemy.ID, Target: &to}
				continue
			}
		}
		at, rule := e.At, Engage
		if p.ours[at] >= p.theirs[at] {
			rule = Pursue
		}
		fights[at] = true
		p.decisions[i] = Decision{Element: e.ID, Rule: rule, Contact: enemy.ID, Target: &at}
	}
	return fights
}

// keep gives each element, in ID order, that is undecided, that may
// accepts by index, and whose standing decision is by rule r, that
// decision's target again, while the target is in the element's reach,
// still accepts it, and claimed does not hold it yet. It marks each target
// kept in claimed and returns them, in the order kept.
func (p *plan) keep(r Rule, claimed map[Location]bool, may func(i int) bool, still func(e Element, at Location) bool) []Location {
	var kept []Location
	for i, e := range p.own {
		d, ok := p.was[e.ID]
		if p.decisions[i].Rule != "" || !may(i) || !ok || d.Rule != r || d.Target == nil {
			continue
		}
		at := *d.Target
		if _, reach := p.dist[i][at]; reach && still(e, at) && !claimed[at] {
			claimed[at] = true
			kept = append(kept, at)
			p.decisions[i] = Decision{Element: e.ID, Rule: r, Target: &at}
		}
	}
	return kept
}

// stalest returns the location of the objective seen longest ago that dist
// reaches and ok accepts, ties going to the nearest, then to the lowest by
// [Location.String], or false when none is.
func stalest(objectives []Objective, dist map[Location]int, ok func(Location) bool) (Location, bool) {
	var best Objective
	found := false
	for _, o := range objectives {
		n, reach := dist[o.At]
		if !reach || !ok(o.At) {
			continue
		}
		if !found || o.Age > best.Age || o.Age == best.Age &&
			(n < dist[best.At] || n == dist[best.At] && o.At.String() < best.At.String()) {
			best, found = o, true
		}
	}
	return best.At, found
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
// adjacent open cell of its sector, holding no contact seen this round and
// none of the faction's own elements, which occupied marks, farthest by
// Chebyshev distance from the nearest known contact outside the fight at
// at, ties going to the first of up, right, down, left. The fight's own
// contacts are one step from every neighbor, so they would tie them all.
// It returns false when no such neighbor is open.
func (m Map) retreat(at Location, contacts []Contact, occupied map[Location]bool) (Location, bool) {
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
		if occupied[to] {
			continue
		}
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
