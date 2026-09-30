package scenario

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// factionState is what the narrator holds of one faction: its squads as
// observed, what it knows, decides, and orders, as far as the narration
// tells what changes in them. Its collectors write what changed into a
// round's block.
type factionState struct {
	name       string
	initial    []element            // its squads in round 0
	ready      bool                 // whether its round-0 observation is in
	latest     []element            // its squads in its latest observation
	regroups   map[string]bool      // its recovering squads already narrated
	losses     map[location]lost    // the objectives it lost
	picture    *assessment          // its last assessment, or nil
	revisions  map[int]revised      // the revisions of its assessments, by round
	directives map[string]directive // its last directive, by element
	moving     *movement            // its orders in effect, as narrated, or nil
	orders     map[int]movement     // its latest orders not yet resolved, by round
}

// revised is the first and the highest revision of a faction's assessments
// of a round.
type revised struct {
	first, highest int
}

func newFactionState(name string) *factionState {
	return &factionState{
		name:      name,
		regroups:  map[string]bool{},
		losses:    map[location]lost{},
		revisions: map[int]revised{},
		orders:    map[int]movement{},
	}
}

// cellOf returns the cell of one of the faction's squads in its latest
// observation.
func (f *factionState) cellOf(id string) (location, bool) {
	for _, s := range f.latest {
		if s.ID == id {
			return s.At, true
		}
	}
	return location{}, false
}

// lostAt reports whether the faction was alerted to an objective lost in
// round.
func (f *factionState) lostAt(round int) bool {
	for _, l := range f.losses {
		if l.Round == round {
			return true
		}
	}
	return false
}

// revise notes an assessment's revision of round.
func (f *factionState) revise(round, revision int) {
	if r, ok := f.revisions[round]; !ok {
		f.revisions[round] = revised{first: revision, highest: revision}
	} else {
		f.revisions[round] = revised{first: r.first, highest: max(r.highest, revision)}
	}
}

// regrouped collects in b each own element an observation shows
// recovering, once for each time it retreats: it sits out the round after
// the observed one.
func (f *factionState) regrouped(d observed, b *block) {
	now := map[string]bool{}
	for _, s := range d.Own {
		if s.Status == "recovering" {
			now[s.ID] = true
		}
	}
	for _, id := range slices.Sorted(maps.Keys(now)) {
		if !f.regroups[id] {
			b.fates[f.name] = append(b.fates[f.name], fmt.Sprintf("%s regroups, sits out %d", id, d.Round+1))
		}
	}
	f.regroups = now
}

// assessed collects in b what an assessment changed in what the faction
// knows: contacts it spots, contacts it loses track of, and objectives it
// sees held differently, or learns of from a loss alert. intelligence
// revises a round's assessment when an alert follows it, so a round may be
// assessed twice; the second adds only what the alert changed. name
// renders a cell.
func (f *factionState) assessed(d assessment, b *block, name func(location) string) {
	var prev assessment
	if f.picture != nil {
		prev = *f.picture
	}
	f.picture = &d
	was := map[string]contact{}
	for _, c := range prev.Contacts {
		was[c.ID] = c
	}
	var changes []string
	for _, c := range d.Contacts {
		if p, ok := was[c.ID]; c.Age == 0 && (!ok || p.Age > 0) {
			changes = append(changes, fmt.Sprintf("spotted %s %d @ %s", c.ID, c.Strength, place(c.At)))
		}
	}
	for _, id := range gone(prev.Contacts, ids(d.Contacts, contactID), contactID) {
		changes = append(changes, "lost "+id)
	}
	believed := map[location]belief{}
	for _, o := range prev.Objectives {
		believed[o.At] = o
	}
	for _, o := range d.Objectives {
		if p, ok := believed[o.At]; ok && p.Holder == o.Holder {
			continue
		}
		state := heldBy(o.Holder)
		if l, ok := f.losses[o.At]; ok && l.Round == o.Seen && l.Holder == o.Holder {
			state = "lost to " + o.Holder
		}
		changes = append(changes, name(o.At)+" "+state)
	}
	if len(changes) > 0 {
		b.knows[f.name] = append(b.knows[f.name], changes...)
	}
}

// directed collects in b each squad whose directive the faction's
// directives change. A squad decided on again in the same round shows only
// its latest directive. name renders a cell.
func (f *factionState) directed(d directives, b *block, name func(location) string) {
	var changed []directive
	changed, f.directives = directiveChanges(f.directives, d.Directives)
	for _, x := range changed {
		r := row(col("id", x.Element))
		verb, target, contact := act(x, f.cellOf, name)
		r.cells = append(r.cells, col("verb", verb))
		if target != "" {
			r.cells = append(r.cells, col("target", target))
		}
		if contact != "" {
			r.cells = append(r.cells, col("contact", contact))
		}
		rows := slices.DeleteFunc(b.decides[f.name], func(p detail) bool { return p.cells[0].text == x.Element })
		b.decides[f.name] = append(rows, r)
	}
}

// movement is the squads a faction's orders move, by how they move.
type movement struct {
	move, retreat, pursue []string
}

// equal reports whether m and o move the same squads, the same way.
func (m movement) equal(o movement) bool {
	return slices.Equal(m.move, o.move) && slices.Equal(m.retreat, o.retreat) && slices.Equal(m.pursue, o.pursue)
}

// String renders a movement: each way squads move, or that all hold when
// none moves.
func (m movement) String() string {
	var out []string
	for _, k := range []struct {
		verb string
		ids  []string
	}{{"move", m.move}, {"retreat", m.retreat}, {"pursue", m.pursue}} {
		if len(k.ids) > 0 {
			out = append(out, k.verb+" "+strings.Join(k.ids, " "))
		}
	}
	if len(out) == 0 {
		return "all hold"
	}
	return strings.Join(out, " · ")
}

// ordered records the faction's orders for their round. operations issues
// a round's orders when it sees the round before, and issues them again
// when a directive changes them. exercise keeps the last orders it
// records, so the theater narrates a round's orders when the round
// resolves, as the orders in effect. It does not narrate orders for a
// round already resolved, the last being resolves, which exercise refuses.
func (f *factionState) ordered(d orders, resolves int) {
	if d.Round <= resolves {
		return
	}
	var m movement
	for _, o := range d.Orders {
		switch {
		case o.Retreat:
			m.retreat = append(m.retreat, o.Element)
		case o.Pursue:
			m.pursue = append(m.pursue, o.Element)
		case len(o.Steps) > 0:
			m.move = append(m.move, o.Element)
		}
	}
	slices.Sort(m.move)
	slices.Sort(m.retreat)
	slices.Sort(m.pursue)
	f.orders[d.Round] = m
}

// inEffect collects in b, the block of the round before round, where the
// faction issued them, the orders it had in effect for round, when the
// squads they move, and how, differ from its last narrated orders, and
// forgets its orders for round and earlier.
func (f *factionState) inEffect(round int, b *block) {
	m, ok := f.orders[round]
	for r := range f.orders {
		if r <= round {
			delete(f.orders, r)
		}
	}
	if !ok {
		return
	}
	if f.moving != nil && f.moving.equal(m) {
		return
	}
	f.moving = &m
	b.orders[f.name] = m.String()
}
