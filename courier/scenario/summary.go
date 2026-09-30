package scenario

import (
	"fmt"
	"slices"
	"strings"
)

// roundBook groups one exercise's assessments and directives by round for
// the summary narration. It narrates a round once every faction has been
// assessed on a later round or, after the exercise concluded, once every
// faction's assessment of the round is in. A round's narration has a block
// per faction, with what changed since the faction's last round and the
// directives decided on it; quiet rounds collapse into one line. On the
// stream, a directive decided on a round follows the round's assessment and
// precedes the next round's, so waiting for the next round puts the
// directive in its round's block.
type roundBook struct {
	factions   []string // the factions narrated, once known
	pending    map[int]map[string]assessmentData
	directives map[int]map[string]directiveData // by the round they are narrated with
	standing   map[string]map[string]directive  // each faction's last directive, by element
	next       int                              // the next round to narrate
	begun      bool                             // whether a round was narrated yet
	prev       map[string]assessmentData
	prevKey    map[string][]string // each faction's last narrated picture, without ages
	quiet      [2]int              // the open run of quiet rounds, or -1s
}

func newRoundBook(faction string) *roundBook {
	b := &roundBook{
		pending:    map[int]map[string]assessmentData{},
		directives: map[int]map[string]directiveData{},
		standing:   map[string]map[string]directive{},
		prev:       map[string]assessmentData{},
		prevKey:    map[string][]string{},
		quiet:      [2]int{-1, -1},
	}
	if faction != "" {
		b.factions = []string{faction}
	}
	return b
}

// add files d under its round.
func (b *roundBook) add(d assessmentData) {
	if b.pending[d.Round] == nil {
		b.pending[d.Round] = map[string]assessmentData{}
	}
	b.pending[d.Round][d.Faction] = d
}

// direct files d under its round, or under the next round to narrate when
// its own round was narrated already, as happens to a directive delayed on
// the stream.
func (b *roundBook) direct(d directiveData) {
	r := max(d.Round, b.next)
	if b.directives[r] == nil {
		b.directives[r] = map[string]directiveData{}
	}
	b.directives[r][d.Faction] = d
}

// flush narrates, in order, every round that is complete: each faction has
// been assessed on a later round, so the round's directives are in and a
// missing assessment will never come. Once the exercise has concluded, a
// round is complete as soon as each faction's assessment of it is in. last
// holds each faction's latest assessed round.
func (b *roundBook) flush(note func(string, ...any), last map[string]int, concluded bool) {
	// The run reads the stream from its start, so it begins at round 0; a
	// round no faction will be assessed on is passed once each faction has
	// been assessed on a later one.
	if len(b.factions) == 0 || len(b.pending) == 0 && !b.begun {
		return
	}
	b.begun = true
	for {
		set := b.pending[b.next]
		for _, f := range b.factions {
			if _, ok := set[f]; !ok || !concluded {
				if r, seen := last[f]; !seen || r <= b.next {
					return
				}
			}
		}
		b.narrate(note, b.next, set)
		delete(b.pending, b.next)
		delete(b.directives, b.next)
		b.next++
	}
}

// end closes an open run of quiet rounds.
func (b *roundBook) end(note func(string, ...any)) {
	switch {
	case b.quiet[0] < 0:
	case b.quiet[0] == b.quiet[1]:
		note("round %d: no change", b.quiet[0])
	default:
		note("rounds %d–%d: no change", b.quiet[0], b.quiet[1])
	}
	b.quiet = [2]int{-1, -1}
}

// narrate notes round r, or extends the run of quiet rounds when no
// faction learned or lost anything: its picture is the same but for the
// ages of what it remembers, which the next narrated round shows.
func (b *roundBook) narrate(note func(string, ...any), r int, set map[string]assessmentData) {
	if len(set) == 0 {
		b.end(note)
		return
	}
	width := 0
	for _, f := range b.factions {
		width = max(width, len(f))
	}
	var blocks [][]string
	quiet := true
	for _, f := range b.factions {
		d, ok := set[f]
		if !ok {
			blocks = append(blocks, []string{fmt.Sprintf("%-*s  no assessment of this round", width, f)})
			quiet = false
			continue
		}
		prev, had := b.prev[f]
		head, changed := header(d, prev, had)
		body := b.body(d, prev, had, r, true)
		if dir, ok := b.directives[r][f]; ok {
			for i, l := range b.directs(dir) {
				label := "           "
				if i == 0 {
					label = "directs    "
				}
				body = append(body, label+l)
			}
		}
		key := b.body(d, prev, had, r, false)
		if changed || len(body) > len(key) || !slices.Equal(key, b.prevKey[f]) {
			quiet = false
		}
		lines := []string{fmt.Sprintf("%-*s  %s", width, f, head)}
		for _, l := range body {
			lines = append(lines, strings.Repeat(" ", width+2)+l)
		}
		blocks = append(blocks, lines)
		b.prev[f] = d
		b.prevKey[f] = key
	}
	if quiet {
		if b.quiet[0] < 0 {
			b.quiet[0] = r
		}
		b.quiet[1] = r
		return
	}
	b.end(note)
	note("round %d", r)
	for _, lines := range blocks {
		for _, l := range lines {
			note("  %s", l)
		}
	}
}

// header renders a faction's own elements, their total strength, and what
// changed since its previous narrated assessment: the strength it lost and
// the elements gone. It reports whether anything changed.
func header(d, prev assessmentData, had bool) (string, bool) {
	strength := 0
	for _, e := range d.Own {
		strength += e.Strength
	}
	h := fmt.Sprintf("%d %s, strength %d", len(d.Own), plural(len(d.Own), "element"), strength)
	if !had {
		return h, true
	}
	before := 0
	for _, e := range prev.Own {
		before += e.Strength
	}
	var delta []string
	if strength != before {
		delta = append(delta, fmt.Sprintf("%+d", strength-before))
	}
	for _, id := range gone(ids(prev.Own, func(e assessedElement) string { return e.ID }), ids(d.Own, func(e assessedElement) string { return e.ID })) {
		delta = append(delta, "lost "+id)
	}
	if len(delta) == 0 {
		return h, false
	}
	return h + " (" + strings.Join(delta, ", ") + ")", true
}

// body renders what a faction knows of the enemy and the objectives: the
// contacts it sees this round, those it remembers at their last-seen cell,
// those intelligence dropped since its previous narrated assessment, and
// each objective by who it believes holds it. Without ages, it renders the
// standing picture a quiet round is compared by: no ages, and no drops,
// which belong to the round they happen in.
func (b *roundBook) body(d, prev assessmentData, had bool, r int, ages bool) []string {
	var sees, remembers, dropped []string
	for _, c := range d.Contacts {
		if c.Age == 0 {
			sees = append(sees, fmt.Sprintf("%s(%d) %s", c.ID, c.Strength, place(c.At)))
		} else {
			s := fmt.Sprintf("%s(%d) at %s", c.ID, c.Strength, place(c.At))
			if ages {
				s += ", " + ago(c.Age)
			}
			remembers = append(remembers, s)
		}
	}
	if had {
		now := ids(d.Contacts, func(c assessedContact) string { return c.ID })
		for _, c := range prev.Contacts {
			if !now[c.ID] {
				dropped = append(dropped, fmt.Sprintf("%s, last seen %s", c.ID, ago(r-c.Seen)))
			}
		}
	}
	lines := []string{"sees       " + orNone(sees, " · ")}
	if len(remembers) > 0 {
		lines = append(lines, "remembers  "+strings.Join(remembers, " · "))
	}
	if ages && len(dropped) > 0 {
		lines = append(lines, "dropped    "+strings.Join(dropped, " · "))
	}
	return append(lines, "objectives "+objectivesLine(d, ages))
}

// objectivesLine groups the objectives by what the faction believes of
// them: held by itself, held by another faction, unheld, or never seen. A
// belief from an earlier round carries its age.
func objectivesLine(d assessmentData, ages bool) string {
	groups := map[string][]string{}
	for _, o := range d.Objectives {
		label := "unknown"
		switch {
		case !o.Known:
		case o.Holder == "":
			label = "unheld"
		case o.Holder == d.Faction:
			label = "held"
		default:
			label = o.Holder
		}
		s := place(o.At)
		if ages && o.Known && o.Age > 0 {
			s += " (" + ago(o.Age) + ")"
		}
		groups[label] = append(groups[label], s)
	}
	var others []string
	for label := range groups {
		if label != "held" && label != "unheld" && label != "unknown" {
			others = append(others, label)
		}
	}
	slices.Sort(others)
	var parts []string
	for _, label := range slices.Concat([]string{"held"}, others, []string{"unheld", "unknown"}) {
		if items := groups[label]; len(items) > 0 {
			parts = append(parts, label+" "+strings.Join(items, " "))
		}
	}
	return orNone(parts, " · ")
}

// directs renders each directive of d that differs from its element's last
// one, and records d as the faction's standing directives. Each line names
// what the element now does and, when it was doing something else, what
// that was.
func (b *roundBook) directs(d directiveData) []string {
	was := b.standing[d.Faction]
	now := make(map[string]directive, len(d.Directives))
	var lines []string
	for _, x := range d.Directives {
		now[x.Element] = x
		prev, ok := was[x.Element]
		if ok && sameDirective(prev, x) {
			continue
		}
		l := x.Element + " " + doing(x, false)
		if ok {
			l += " (was " + doing(prev, true) + ")"
		}
		lines = append(lines, l)
	}
	b.standing[d.Faction] = now
	return lines
}

func sameDirective(a, b directive) bool {
	return a.Rule == b.Rule && a.Contact == b.Contact && (a.Target == nil) == (b.Target == nil) &&
		(a.Target == nil || *a.Target == *b.Target)
}

// doing renders what a directive has its element do, in the present tense
// ("engages b2 at a:5,5") or, when was is set, as a participle
// ("engaging b2 at a:5,5"). A directive without a rule, as courier's
// stand-in issues, heads for its target or holds.
func doing(x directive, was bool) string {
	verb := func(present, participle string) string {
		if was {
			return participle
		}
		return present
	}
	switch {
	case x.Target == nil:
		return verb("holds", "holding")
	case x.Rule == "engage":
		return verb("engages ", "engaging ") + x.Contact + " at " + place(*x.Target)
	case x.Rule == "secure":
		return verb("secures ", "securing ") + place(*x.Target)
	}
	return verb("heads for ", "heading for ") + place(*x.Target)
}

func ago(n int) string { return fmt.Sprintf("%d %s ago", n, plural(n, "round")) }

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

// ids returns the set of IDs of xs.
func ids[T any](xs []T, id func(T) string) map[string]bool {
	out := make(map[string]bool, len(xs))
	for _, x := range xs {
		out[id(x)] = true
	}
	return out
}
