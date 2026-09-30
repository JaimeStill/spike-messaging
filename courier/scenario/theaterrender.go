package scenario

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// tell narrates the initial conditions once the narration's step has
// begun, and the start and each faction's round-0 observation are in.
func (n *narrator) tell() {
	s := n.world.setup
	if !n.cursor.open || n.cursor.told || s == nil {
		return
	}
	for _, f := range s.Factions {
		if !n.faction(f).ready {
			return
		}
	}
	n.cursor.told = true
	var sizes []string
	for _, sec := range s.Map.Sectors {
		size := fmt.Sprintf("%dx%d", sec.Width, sec.Height)
		if len(s.Map.Sectors) > 1 {
			size = sec.ID + " " + size
		}
		sizes = append(sizes, size)
	}
	objectives := row(col("key", "objectives"), apart("value", "none"))
	if len(n.world.sites) > 0 {
		objectives = row(col("key", "objectives"), apart("value", "hidden from both factions"))
		for _, l := range n.world.sites {
			objectives.sub = append(objectives.sub, text(n.world.name(l)))
		}
	}
	conditions := []detail{
		row(col("key", "map size"), apart("value", strings.Join(sizes, ", "))),
		row(col("key", "seed"), apart("value", strconv.FormatInt(n.world.seed, 10))),
		objectives,
	}
	for _, f := range s.Factions {
		conditions = append(conditions, row(col("key", f)).with(squadRows(n.faction(f).initial, true)...))
	}
	n.note("%s: %d rounds at %s", s.Name, s.RoundLimit, time.Duration(s.RoundIntervalMS)*time.Millisecond)
	for _, l := range list(conditions, "  ") {
		n.note("%s", l)
	}
	n.note("")
}

// conditions returns the final conditions: the verdict, who holds each
// objective, each faction's surviving squads and losses, the events on the
// stream by type, the chain's latency, and how to check the run. It
// narrates nothing: the caller finishes the narration first.
func (n *narrator) conditions() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	var out []detail
	if n.end != nil {
		out = append(out, text("verdict  "+outcome(*n.end)))
	}
	if n.world.setup != nil {
		objectives := text("objectives")
		if len(n.world.sites) == 0 {
			objectives = text("objectives  none")
		}
		for _, l := range n.world.sites {
			objectives = objectives.with(row(col("objective", n.world.name(l)), apart("holder", orUnheld(n.world.holders[l]))))
		}
		out = append(out, objectives)
		for _, name := range n.world.setup.Factions {
			f := n.faction(name)
			total := 0
			for _, s := range f.latest {
				total += s.Strength
			}
			head := fmt.Sprintf("%s  strength %d", name, total)
			if lost := gone(f.initial, ids(f.latest, elementID), elementID); len(lost) > 0 {
				head += ", lost " + strings.Join(lost, " ")
			}
			out = append(out, text(head).with(squadRows(f.latest, false)...))
		}
	}
	total := 0
	var kinds []detail
	for _, t := range []string{startedType, resolvedType, lostType, observedType, assessmentType, directiveType, ordersType, concludedType} {
		total += n.ledger.counts[t]
		kinds = append(kinds, row(col("type", t), part{key: "count", text: strconv.Itoa(n.ledger.counts[t]), apart: true, right: true}))
	}
	out = append(out, text(fmt.Sprintf("events  %d", total)).with(kinds...))
	out = append(out, text("chain p50").with(
		row(col("hop", "observed -> assessed"), apart("p50", p50(n.ledger.hops(observedType, assessmentType, 0)))),
		row(col("hop", "assessed -> directed"), apart("p50", p50(n.ledger.hops(assessmentType, directiveType, 0))+" (rounds with a directive)")),
		row(col("hop", "directed -> ordered"), apart("p50", p50(n.ledger.hops(directiveType, ordersType, 1)))),
	))
	out = append(out, text("check  mise run demo-theater-check "+n.exercise))
	return list(out, "")
}

// outcome renders how the exercise ended.
func outcome(c concluded) string {
	after := fmt.Sprintf(" after round %d", c.Round)
	switch {
	case c.Reason == "stopped":
		return "stopped" + after
	case c.Winner == "" && c.Reason == "limit":
		return "a draw at the round limit, neither side holding more objectives"
	case c.Winner == "":
		return "a draw by " + c.Reason + after
	case c.Reason == "limit":
		return c.Winner + " wins at the round limit, holding more objectives"
	case c.Reason == "objectives":
		return c.Winner + " wins holding every objective" + after
	case c.Reason == "elimination":
		return c.Winner + " wins by elimination" + after
	}
	return c.Winner + " wins by " + c.Reason + after
}

// heldBy renders an objective's holder as a state: held by the holder, or
// unheld.
func heldBy(holder string) string {
	if holder == "" {
		return "unheld"
	}
	return "held by " + holder
}

// orUnheld renders an objective's holder, or unheld.
func orUnheld(holder string) string {
	if holder == "" {
		return "unheld"
	}
	return holder
}

// squadRows renders a faction's squads by ID, with their kind and the
// health of their operators, and their cell when at is set.
func squadRows(ss []element, at bool) []detail {
	ss = slices.Clone(ss)
	slices.SortFunc(ss, func(a, b element) int { return cmp.Compare(a.ID, b.ID) })
	var out []detail
	for _, s := range ss {
		r := row(col("id", s.ID), col("kind", s.Kind), col("health", health(s.Health)))
		if at {
			r.cells = append(r.cells, col("at", "@ "+place(s.At)))
		}
		out = append(out, r)
	}
	return out
}

// health renders the health of a squad's operators: NxH when each of its N
// operators has health H, otherwise each operator's health.
func health(hs []int) string {
	if len(hs) == 0 {
		return "none"
	}
	parts := make([]string, len(hs))
	for i, h := range hs {
		parts[i] = strconv.Itoa(h)
	}
	if slices.Min(hs) == slices.Max(hs) {
		return fmt.Sprintf("%dx%d", len(hs), hs[0])
	}
	return strings.Join(parts, ",")
}

// swing renders an element's strength through a fight or a retreat, followed
// by the operators it lost, or by "destroyed" when it was.
func swing(e engaged) string {
	s := fmt.Sprintf("%s %d->%d", e.ID, e.Before, e.After)
	switch {
	case e.After == 0:
		s += " destroyed"
	case e.Fallen > 0:
		s += fmt.Sprintf(" (%d down)", e.Fallen)
	}
	return s
}
