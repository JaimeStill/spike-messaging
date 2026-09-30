package scenario

import (
	"cmp"
	"maps"
	"slices"
	"strings"
)

// block is what one round changed, as the narration tells it: the
// observer's record of the round, and what each faction knows, decides,
// and orders in it.
type block struct {
	observer []labeled                    // fights and retreats, one row each
	shown    map[string]bool              // the elements the observer's rows show destroyed
	results  map[string]*objectiveResults // each faction's objective results
	fates    map[string][]string          // each faction's elements destroyed or regrouping
	knows    map[string][]string          // each faction's assessment changes
	decides  map[string][]detail          // each faction's directive changes, one per element
	orders   map[string]string            // each faction's orders, as issued in the round
}

// objectiveResults is a faction's objective results in a round: the
// objectives it captures, those it is taking, and those it loses.
type objectiveResults struct {
	captures, takes, loses []string
}

// items renders the results, each verb with its objectives, in the order
// captures, takes, loses. A nil r renders none.
func (r *objectiveResults) items() []string {
	if r == nil {
		return nil
	}
	var out []string
	for _, v := range []struct {
		verb string
		what []string
	}{{"captures", r.captures}, {"takes", r.takes}, {"loses", r.loses}} {
		if len(v.what) > 0 {
			out = append(out, v.verb+" "+strings.Join(v.what, " · "))
		}
	}
	return out
}

// at returns the block of round.
func (n *narrator) at(round int) *block {
	b := n.cursor.blocks[round]
	if b == nil {
		b = &block{
			shown: map[string]bool{}, results: map[string]*objectiveResults{}, fates: map[string][]string{},
			knows: map[string][]string{}, decides: map[string][]detail{}, orders: map[string]string{},
		}
		n.cursor.blocks[round] = b
	}
	return b
}

// results returns a faction's objective results in the block of round.
func (n *narrator) results(round int, faction string) *objectiveResults {
	b := n.at(round)
	if b.results[faction] == nil {
		b.results[faction] = &objectiveResults{}
	}
	return b.results[faction]
}

// side is one side of a block: the observer, or a faction, with its rows.
type side struct {
	name string
	rows []labeled
}

// sides returns the sides of a block that have rows. The observer's side
// comes first, with the fights, the retreats, and each faction's results.
// Each faction's side follows, in the exercise's order of factions, with its
// knows, decides (by squad), and orders rows.
func (n *narrator) sides(b *block) []side {
	factions := n.factions(b)
	observer := side{name: "observer", rows: slices.Clone(b.observer)}
	for _, f := range factions {
		items := append(b.results[f].items(), b.fates[f]...)
		if len(items) > 0 {
			observer.rows = append(observer.rows, labeled{label: f, items: items})
		}
	}
	var out []side
	if len(observer.rows) > 0 {
		out = append(out, observer)
	}
	for _, f := range factions {
		s := side{name: f}
		if len(b.knows[f]) > 0 {
			s.rows = append(s.rows, labeled{label: "knows", items: b.knows[f]})
		}
		if len(b.decides[f]) > 0 {
			decides := slices.SortedFunc(slices.Values(b.decides[f]), func(a, b detail) int { return cmp.Compare(a.cells[0].text, b.cells[0].text) })
			s.rows = append(s.rows, labeled{label: "decides", items: list(decides, ""), each: true})
		}
		if o := b.orders[f]; o != "" {
			s.rows = append(s.rows, labeled{label: "orders", items: []string{o}})
		}
		if len(s.rows) > 0 {
			out = append(out, s)
		}
	}
	return out
}

// factions returns the exercise's factions, then any other faction a block
// names, sorted.
func (n *narrator) factions(b *block) []string {
	out := slices.Clone(n.world.factions())
	var others []string
	for _, f := range slices.Concat(
		slices.Collect(maps.Keys(b.results)), slices.Collect(maps.Keys(b.fates)), slices.Collect(maps.Keys(b.knows)),
		slices.Collect(maps.Keys(b.decides)), slices.Collect(maps.Keys(b.orders)),
	) {
		if !slices.Contains(out, f) && !slices.Contains(others, f) {
			others = append(others, f)
		}
	}
	slices.Sort(others)
	return append(out, others...)
}

// rowLabelWidth is the width of the longest of a block's own row labels,
// retreat and decides.
const rowLabelWidth = len("retreat")

// labelWidth is the width to which a row's label is padded: the longest of
// the rows' own labels and the faction names.
func (n *narrator) labelWidth() int {
	w := rowLabelWidth
	for _, f := range n.world.factions() {
		w = max(w, len(f))
	}
	return w
}

// rows renders a row at indent, its label padded to the rows' width.
func (n *narrator) rows(indent string, l labeled) []string {
	return l.lines(indent, n.labelWidth())
}
