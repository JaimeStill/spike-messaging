package rules

import (
	"cmp"
	"maps"
	"math/rand/v2"
	"slices"
)

// The constants of a fight and of a capture.
const (
	// HitChance is the percentage chance that an operator's shot hits.
	HitChance = 50
	// MinDamage and MaxDamage bound the health a hit takes, inclusive.
	MinDamage = 20
	MaxDamage = 60
	// CaptureRounds is the rounds in a row a faction ends alone on an
	// objective to take it.
	CaptureRounds = 2
)

// Order is a faction's command to one element for one round: the cells it
// enters, in order. An order with no steps holds the element in place.
// Retreat marks the one order that moves an engaged element: one step out
// of its fight. Pursue has an element that stays in its fight fire on an
// enemy that retreats from it, and draw that enemy's fire in return.
type Order struct {
	Element string     `json:"element"`
	Steps   []Location `json:"steps"`
	Retreat bool       `json:"retreat,omitempty"`
	Pursue  bool       `json:"pursue,omitempty"`
}

// Resolve resolves round of s under orders: it moves, exchanges fire between
// each retreat and its pursuers, fights, and captures, then observes the
// next state for each faction and judges it against limit, the exercise's
// round limit. Every random draw comes from seed and round alone, so a round
// resolves the same way each time it is resolved. It returns the next state,
// the observations indexed like s.Factions, the verdict, and the round's
// [Resolution].
// Resolve does not change s or orders; the next state shares no slice or
// map with s, and its elements are sorted by ID.
func Resolve(s State, seed int64, round, limit int, orders []Order) (next State, obs [2]Observation, v Verdict, res Resolution) {
	rng := rand.New(rand.NewPCG(uint64(seed), uint64(round)))
	next = s.clone()
	retreats := move(&next, orders)
	res.Retreats = volley(&next, retreats, orders, rng)
	res.Engagements = fight(&next, rng)
	res.Losses = bury(&next)
	settle(&next, retreats)
	res.Captures, res.Progress = capture(&next)
	res.Objectives = objectives(next)
	return next, Observe(next, round), Judge(next, round, limit), res
}

// contested returns the cells, by [Location.Key], that hold both factions'
// elements.
func contested(es []Element) map[string]bool {
	factions := make(map[string]map[string]bool)
	for _, e := range es {
		k := e.At.Key()
		if factions[k] == nil {
			factions[k] = make(map[string]bool)
		}
		factions[k][e.Faction] = true
	}
	out := make(map[string]bool)
	for k, fs := range factions {
		if len(fs) > 1 {
			out[k] = true
		}
	}
	return out
}

// Resolution is what a round's resolution did, as the umpire records it:
// each retreat and the exchange it drew, each fight, each element
// destroyed, each objective that changed hands, and each objective a
// faction is taking, and then every objective with its holder, which only
// the umpire knows. Every list is empty, not nil, for a round where
// nothing happened.
type Resolution struct {
	Retreats    []Retreat         `json:"retreats"`
	Engagements []Engagement      `json:"engagements"`
	Losses      []Loss            `json:"losses"`
	Captures    []Capture         `json:"captures"`
	Progress    []Advance         `json:"progress"`
	Objectives  []ObjectiveStatus `json:"objectives"`
}

// Retreat is an element that left its fight, and the exchange with the
// enemy that pursued it: its strength before and after, the operators it
// lost, and each pursuer's strength before and after the fire returned.
// After is 0 when the exchange destroyed it. A retreat no one pursued has
// no pursuers, and loses nothing.
type Retreat struct {
	ID       string    `json:"id"`
	Faction  string    `json:"faction"`
	From     Location  `json:"from"`
	To       Location  `json:"to"`
	Before   int       `json:"before"`
	After    int       `json:"after"`
	Fallen   int       `json:"fallen"`
	Pursuers []Engaged `json:"pursuers"`
}

// Engagement is one cell's fight in a round: every element in the cell, of
// both factions.
type Engagement struct {
	At       Location  `json:"at"`
	Elements []Engaged `json:"elements"`
}

// Engaged is one element of an [Engagement]: its strength before and after
// the round's fire, and the operators it lost. After is 0 when the element
// was destroyed.
type Engaged struct {
	ID      string `json:"id"`
	Faction string `json:"faction"`
	Before  int    `json:"before"`
	After   int    `json:"after"`
	Fallen  int    `json:"fallen"`
}

// Capture is an objective that changed hands. Faction holds it now, and From
// held it before ("" when it was unheld).
type Capture struct {
	At      Location `json:"at"`
	Faction string   `json:"faction"`
	From    string   `json:"from"`
}

// Advance is an objective a faction is taking but does not hold yet: the
// rounds in a row it has ended alone on it, fewer than [CaptureRounds].
type Advance struct {
	At      Location `json:"at"`
	Faction string   `json:"faction"`
	Rounds  int      `json:"rounds"`
}

// Loss is an element destroyed in a retreat's pursuit or in a fight.
type Loss struct {
	ID      string `json:"id"`
	Faction string `json:"faction"`
}

// move applies every order at once. It refuses the orders the rules refuse,
// every order of a recovering element, and every order of an element pinned
// in a fight but the one-step retreat. It returns the elements that
// retreated, by ID, with the cell each left.
func move(s *State, orders []Order) map[string]Location {
	byID := make(map[string]Order, len(orders))
	for _, o := range orders {
		byID[o.Element] = o
	}
	pinned := contested(s.Elements)
	final := make([]Location, len(s.Elements))
	for i, e := range s.Elements {
		final[i] = e.At
		o, ok := byID[e.ID]
		switch {
		case !ok, e.Status == StatusRecovering:
			continue
		case pinned[e.At.Key()] && (!o.Retreat || len(o.Steps) != 1):
			continue
		}
		if to, ok := s.Map.walk(e, o.Steps); ok {
			final[i] = to
		}
	}
	for refused := true; refused; {
		refused = false
		ends := make(map[string]int, len(final))
		for i, e := range s.Elements {
			ends[e.Faction+"@"+final[i].Key()]++
		}
		for i, e := range s.Elements {
			if final[i] != e.At && !pinned[final[i].Key()] && ends[e.Faction+"@"+final[i].Key()] > 1 {
				final[i] = e.At
				refused = true
			}
		}
	}
	retreats := make(map[string]Location)
	for i, e := range s.Elements {
		if final[i] != e.At && pinned[e.At.Key()] {
			retreats[e.ID] = e.At
		}
		s.Elements[i].At = final[i]
	}
	return retreats
}

// walk returns where e ends after steps, or false when the rules refuse the
// order: more steps than e's kind allows, or a step that is neither an
// orthogonal adjacency in the sector nor a traversal of the gate e stands
// on, or that leaves the grid or enters an obstacle.
func (m Map) walk(e Element, steps []Location) (Location, bool) {
	if len(steps) > e.Kind.Moves() {
		return Location{}, false
	}
	at := e.At
	for _, to := range steps {
		sec, ok := m.sector(at.Sector)
		if !ok {
			return Location{}, false
		}
		if g, ok := sec.gate(at.Point); ok && to == g.To {
			sec, ok = m.sector(to.Sector)
			if !ok {
				return Location{}, false
			}
		} else if to.Sector != at.Sector || abs(to.X-at.X)+abs(to.Y-at.Y) != 1 {
			return Location{}, false
		}
		if !sec.inside(to.Point) || sec.obstacle(to.Point) {
			return Location{}, false
		}
		at = to
	}
	return at, true
}

// operator is one living operator: the element it belongs to, by index into
// the state's elements, and its place in the element's health.
type operator struct{ element, slot int }

// shoot has each shooter fire once at a random target, and returns the
// damage each target takes, keyed by operator. It draws in shooter order,
// the hit first and then, on a hit, the target and the damage.
func shoot(shooters, targets []operator, rng *rand.Rand) map[operator]int {
	damage := make(map[operator]int)
	if len(targets) == 0 {
		return damage
	}
	for range shooters {
		if rng.IntN(100) >= HitChance {
			continue
		}
		t := targets[rng.IntN(len(targets))]
		damage[t] += MinDamage + rng.IntN(MaxDamage-MinDamage+1)
	}
	return damage
}

// operators returns the living operators of the elements at the given
// indexes, in element ID order and then in the order each fields them. An
// operator felled earlier this round, in a pursuit, is no longer living.
func operators(es []Element, at []int) []operator {
	at = slices.Clone(at)
	slices.SortFunc(at, func(a, b int) int { return cmp.Compare(es[a].ID, es[b].ID) })
	var out []operator
	for _, i := range at {
		for slot, h := range es[i].Health {
			if h > 0 {
				out = append(out, operator{i, slot})
			}
		}
	}
	return out
}

// wound applies damage to the operators it keys. An operator reduced to 0
// or below keeps its slot at 0 until [bury] removes it.
func wound(es []Element, damage map[operator]int) {
	for op, d := range damage {
		es[op.element].Health[op.slot] = max(es[op.element].Health[op.slot]-d, 0)
	}
}

// volley plays out the pursuit of each retreat. Each element of the other
// faction that stays in the cell a retreat left, and whose order pursues,
// fires once with all its operators at the retreating element, and the
// retreating element's operators fire back once at the pursuers' operators,
// all at once. A retreat no one pursues draws no fire. Retreats resolve in
// ID order.
func volley(s *State, retreats map[string]Location, orders []Order, rng *rand.Rand) []Retreat {
	pursues := make(map[string]bool, len(orders))
	for _, o := range orders {
		pursues[o.Element] = o.Pursue
	}
	out := []Retreat{}
	for _, id := range sortedKeys(retreats) {
		from := retreats[id]
		i := slices.IndexFunc(s.Elements, func(e Element) bool { return e.ID == id })
		e := s.Elements[i]
		var pursuers []int
		for j, o := range s.Elements {
			if o.Faction != e.Faction && o.At == from && pursues[o.ID] {
				pursuers = append(pursuers, j)
			}
		}
		slices.SortFunc(pursuers, func(a, b int) int { return cmp.Compare(s.Elements[a].ID, s.Elements[b].ID) })
		before := make(map[int][2]int, len(pursuers)+1)
		for _, j := range append([]int{i}, pursuers...) {
			before[j] = [2]int{sum(s.Elements[j].Health), living(s.Elements[j].Health)}
		}
		them, us := operators(s.Elements, pursuers), operators(s.Elements, []int{i})
		damage := [2]map[operator]int{shoot(them, us, rng), shoot(us, them, rng)}
		wound(s.Elements, damage[0])
		wound(s.Elements, damage[1])
		r := Retreat{ID: id, Faction: e.Faction, From: from, To: e.At, Pursuers: []Engaged{}}
		r.Before, r.After, r.Fallen = before[i][0], sum(s.Elements[i].Health), before[i][1]-living(s.Elements[i].Health)
		for _, j := range pursuers {
			p := s.Elements[j]
			r.Pursuers = append(r.Pursuers, Engaged{
				ID: p.ID, Faction: p.Faction,
				Before: before[j][0], After: sum(p.Health), Fallen: before[j][1] - living(p.Health),
			})
		}
		out = append(out, r)
	}
	return out
}

// fight has every living operator in each cell that holds both factions'
// elements fire once at a random living enemy operator there, all at once:
// every target is chosen, and every hit lands, before anyone falls. Cells
// fight in key order, and each faction's shooters in element ID order.
func fight(s *State, rng *rand.Rand) []Engagement {
	cells := make(map[string][]int)
	for i, e := range s.Elements {
		cells[e.At.Key()] = append(cells[e.At.Key()], i)
	}
	out := []Engagement{}
	for _, key := range sortedKeys(cells) {
		var sides [2][]int
		for _, i := range cells[key] {
			if s.Elements[i].Faction == s.Factions[0] {
				sides[0] = append(sides[0], i)
			} else {
				sides[1] = append(sides[1], i)
			}
		}
		if len(sides[0]) == 0 || len(sides[1]) == 0 {
			continue
		}
		here := slices.Clone(cells[key])
		slices.SortFunc(here, func(a, b int) int { return cmp.Compare(s.Elements[a].ID, s.Elements[b].ID) })
		before := make(map[int][2]int, len(here))
		for _, i := range here {
			before[i] = [2]int{sum(s.Elements[i].Health), living(s.Elements[i].Health)}
		}
		ops := [2][]operator{operators(s.Elements, sides[0]), operators(s.Elements, sides[1])}
		damage := [2]map[operator]int{shoot(ops[0], ops[1], rng), shoot(ops[1], ops[0], rng)}
		wound(s.Elements, damage[0])
		wound(s.Elements, damage[1])
		g := Engagement{At: s.Elements[here[0]].At}
		for _, i := range here {
			e := s.Elements[i]
			g.Elements = append(g.Elements, Engaged{
				ID: e.ID, Faction: e.Faction,
				Before: before[i][0], After: sum(e.Health), Fallen: before[i][1] - living(e.Health),
			})
		}
		out = append(out, g)
	}
	return out
}

// bury removes every fallen operator, and every element with none left,
// and sets each survivor's strength to its health. It leaves s's elements
// sorted by ID, and returns the elements destroyed, in ID order.
func bury(s *State) []Loss {
	losses := []Loss{}
	var live []Element
	for _, e := range s.Elements {
		e.Health = slices.DeleteFunc(e.Health, func(h int) bool { return h <= 0 })
		e.Strength = sum(e.Health)
		if len(e.Health) == 0 {
			losses = append(losses, Loss{ID: e.ID, Faction: e.Faction})
			continue
		}
		live = append(live, e)
	}
	slices.SortFunc(live, func(a, b Element) int { return cmp.Compare(a.ID, b.ID) })
	slices.SortFunc(losses, func(a, b Loss) int { return cmp.Compare(a.ID, b.ID) })
	s.Elements = live
	return losses
}

// settle sets each element's status for the next round: recovering when it
// retreated this round, engaged when its cell holds the other faction's
// elements, and ready otherwise.
func settle(s *State, retreats map[string]Location) {
	fights := contested(s.Elements)
	for i, e := range s.Elements {
		switch _, retreated := retreats[e.ID]; {
		case retreated:
			s.Elements[i].Status = StatusRecovering
		case fights[e.At.Key()]:
			s.Elements[i].Status = StatusEngaged
		default:
			s.Elements[i].Status = StatusReady
		}
	}
}

// capture counts, for each objective, the rounds in a row one faction has
// ended alone on it, and gives the objective to a faction whose count
// reaches [CaptureRounds]. A round that ends with both factions on the
// objective, or with neither, resets its count, and a faction that holds
// the objective keeps no count on it. It returns each objective that
// changed hands and each count still short of a capture, both in the map's
// objective order.
func capture(s *State) ([]Capture, []Advance) {
	on := make(map[string]map[string]bool)
	for _, e := range s.Elements {
		k := e.At.Key()
		if on[k] == nil {
			on[k] = make(map[string]bool)
		}
		on[k][e.Faction] = true
	}
	captures, progress := []Capture{}, []Advance{}
	for _, o := range s.Map.objectives() {
		k := o.Key()
		fs := on[k]
		if len(fs) != 1 {
			delete(s.Progress, k)
			continue
		}
		f := slices.Collect(maps.Keys(fs))[0]
		from := s.Holders[k]
		if from == f {
			delete(s.Progress, k)
			continue
		}
		p := s.Progress[k]
		if p.Faction != f {
			p = Progress{Faction: f}
		}
		p.Rounds++
		if p.Rounds < CaptureRounds {
			s.Progress[k] = p
			progress = append(progress, Advance{At: o, Faction: f, Rounds: p.Rounds})
			continue
		}
		delete(s.Progress, k)
		s.Holders[k] = f
		captures = append(captures, Capture{At: o, Faction: f, From: from})
	}
	return captures, progress
}

// objectives returns every objective of s with its holder, in the map's
// objective order.
func objectives(s State) []ObjectiveStatus {
	out := []ObjectiveStatus{}
	for _, o := range s.Map.objectives() {
		out = append(out, ObjectiveStatus{At: o, Holder: s.Holders[o.Key()]})
	}
	return out
}

// sum returns the total of hs.
func sum(hs []int) int {
	n := 0
	for _, h := range hs {
		n += h
	}
	return n
}

// living returns how many of hs are above 0.
func living(hs []int) int {
	n := 0
	for _, h := range hs {
		if h > 0 {
			n++
		}
	}
	return n
}

// sortedKeys returns m's keys in ascending order.
func sortedKeys[V any](m map[string]V) []string {
	return slices.Sorted(maps.Keys(m))
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
