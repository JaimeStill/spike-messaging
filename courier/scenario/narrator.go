package scenario

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"

	"github.com/JaimeStill/spike-messaging/core/event"
)

// narrator follows one exercise's events and narrates them. It narrates the
// initial conditions once the start and round 0's observations are in (at
// the start it reads the seed, the rules, and the objectives from
// exercise's API), then each round as a block, and, on demand, the final
// conditions. It collects what each event changes in the block of the round
// the event belongs to, and narrates a block once an event of a later round
// arrives, so a block tells a round's story in the round's order rather
// than the events'. It tells a change that arrives for a round already
// narrated as late, in the next block. It records each event's type and time
// for the ledger. Its methods are safe to call concurrently from the
// reactor's goroutine and the scenario's.
type narrator struct {
	exercise                    string
	note                        func(string, ...any)
	read                        func(context.Context) (exerciseView, error) // reads the exercise's view from its API
	started, concluded, settled *signal

	mu        sync.Mutex
	world     world                    // the observer's world: the start, the view read with it, and the objectives
	byFaction map[string]*factionState // what each faction was observed, knows, decides, and orders
	cursor    cursor                   // the rounds narrated, and the blocks still collecting
	ledger    ledger                   // the events by type, and their times
	end       *concluded
}

// cursor is where the narration stands in the exercise's rounds.
type cursor struct {
	open    bool           // whether the narration's step has begun
	told    bool           // whether the initial conditions were narrated
	blocks  map[int]*block // the rounds not yet narrated, and late changes
	seen    int            // the latest round an event belongs to
	printed int            // the last round narrated, or -1
}

func newNarrator(exercise string) *narrator {
	return &narrator{
		exercise:  exercise,
		note:      func(string, ...any) {},
		read:      func(context.Context) (exerciseView, error) { return exerciseView{}, nil },
		started:   newSignal(),
		concluded: newSignal(),
		settled:   newSignal(),
		world:     world{holders: map[location]string{}},
		byFaction: map[string]*factionState{},
		cursor:    cursor{blocks: map[int]*block{}, printed: -1},
		ledger:    newLedger(),
	}
}

// faction returns what the narrator holds of a faction, starting it empty.
func (n *narrator) faction(name string) *factionState {
	f := n.byFaction[name]
	if f == nil {
		f = newFactionState(name)
		n.byFaction[name] = f
	}
	return f
}

// handle collects one event of the stream in the block of the round it
// belongs to, ignoring events of any other exercise, and narrates the blocks
// it completes. It counts an event before it decodes it, so it counts a
// malformed event and then fails permanently on it.
func (n *narrator) handle(ctx context.Context, e event.Event) error {
	if exerciseOf(e) != n.exercise {
		return nil
	}
	// The objectives and the rules are read before any later event is
	// handled, so every cell is named, and every capture counted, as it is
	// told.
	var view exerciseView
	if e.Type == startedType {
		var err error
		if view, err = n.read(ctx); err != nil {
			return fmt.Errorf("read exercise %s: %w", n.exercise, err)
		}
		if err := view.Rules.validate(); err != nil {
			return fmt.Errorf("read exercise %s: %w", n.exercise, err)
		}
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.ledger.counts[e.Type]++
	if err := n.apply(e, view); err != nil {
		return err
	}
	n.tell()
	n.advance(n.cursor.seen)
	n.settle()
	return nil
}

// apply decodes e and applies it by its type. view is the observer's view,
// read when e is the start. The caller holds mu.
func (n *narrator) apply(e event.Event, view exerciseView) error {
	switch e.Type {
	case startedType:
		return applyAs(e, func(d started) { n.applyStarted(d, view) })
	case resolvedType:
		return applyAs(e, n.applyResolved)
	case lostType:
		return applyAs(e, n.applyLost)
	case observedType:
		return applyAs(e, func(d observed) { n.applyObserved(e, d) })
	case assessmentType:
		return applyAs(e, func(d assessment) { n.applyAssessment(e, d) })
	case directiveType:
		return applyAs(e, func(d directives) { n.applyDirectives(e, d) })
	case ordersType:
		return applyAs(e, func(d orders) { n.applyOrders(e, d) })
	case concludedType:
		return applyAs(e, n.applyConcluded)
	}
	return nil
}

// applyAs decodes e as a T and applies it, or returns the permanent failure
// to decode it.
func applyAs[T any](e event.Event, apply func(T)) error {
	d, err := decode[T](e)
	if err != nil {
		return err
	}
	apply(d)
	return nil
}

// applyStarted takes the start, and the observer's view read with it.
func (n *narrator) applyStarted(d started, view exerciseView) {
	n.world.setup = &d
	n.world.learn(view)
	n.started.fire()
}

// applyResolved collects the observer's record of a round: the orders each
// faction had in effect for it, in the block of the round before, where
// they were issued; then each fight, each retreat, each squad destroyed,
// each objective a faction is taking, and each objective that changed
// hands. The captures update the holders before any cell is named.
func (n *narrator) applyResolved(d resolved) {
	n.belongs(d.Round)
	n.world.resolves = d.Round
	for _, c := range d.Captures {
		n.world.capture(c.At, c.Faction)
	}
	for _, f := range n.world.factions() {
		n.faction(f).inEffect(d.Round, n.at(d.Round-1))
	}
	b := n.at(d.Round)
	show := func(e engaged) string {
		if e.After == 0 {
			b.shown[e.ID] = true
		}
		return swing(e)
	}
	for _, g := range d.Engagements {
		items := []string{n.world.name(g.At)}
		for _, e := range g.Elements {
			items = append(items, show(e))
		}
		b.observer = append(b.observer, labeled{label: "fight", items: []string{strings.Join(items, "  ")}})
	}
	for _, t := range d.Retreats {
		what := t.ID + " " + n.world.name(t.From) + " -> " + n.world.name(t.To)
		if len(t.Pursuers) == 0 {
			what += " unpursued"
		} else {
			fire := []string{show(t.engaged)}
			for _, p := range t.Pursuers {
				fire = append(fire, show(p))
			}
			what += " pursued  " + strings.Join(fire, ", ")
		}
		b.observer = append(b.observer, labeled{label: "retreat", items: []string{what}})
	}
	for _, c := range d.Captures {
		what := n.world.name(c.At)
		if c.From != "" {
			what += " from " + c.From
		}
		r := n.results(d.Round, c.Faction)
		r.captures = append(r.captures, what)
	}
	for _, a := range d.Progress {
		r := n.results(d.Round, a.Faction)
		r.takes = append(r.takes, fmt.Sprintf("%s %d/%d", n.world.name(a.At), a.Rounds, n.world.captureRounds))
	}
	for _, l := range d.Losses {
		if !b.shown[l.ID] {
			b.fates[l.Faction] = append(b.fates[l.Faction], l.ID+" destroyed")
		}
	}
}

// applyLost collects a faction's alert to an objective it lost, and the
// objective's new holder.
func (n *narrator) applyLost(d lost) {
	n.belongs(d.Round)
	n.faction(d.Faction).losses[d.At] = d
	n.world.capture(d.At, d.Holder)
	r := n.results(d.Round, d.Faction)
	r.loses = append(r.loses, n.world.name(d.At)+" to "+d.Holder)
}

// applyObserved takes a faction's observation of its own squads, and
// collects each squad it shows regrouping.
func (n *narrator) applyObserved(e event.Event, d observed) {
	n.belongs(d.Round)
	n.ledger.stamp(e, d.Faction, d.Round)
	f := n.faction(d.Faction)
	f.latest = d.Own
	f.regrouped(d, n.at(d.Round))
	if d.Round == 0 {
		f.initial = d.Own
		f.ready = true
	}
}

// applyAssessment collects what a faction's assessment changed in what it
// knows, and notes its revision.
func (n *narrator) applyAssessment(e event.Event, d assessment) {
	n.belongs(d.Round)
	n.ledger.stamp(e, d.Faction, d.Round)
	f := n.faction(d.Faction)
	f.assessed(d, n.at(d.Round), n.world.name)
	f.revise(d.Round, d.Revision)
}

// applyDirectives collects each squad whose directive a faction's
// directives change.
func (n *narrator) applyDirectives(e event.Event, d directives) {
	n.belongs(d.Round)
	n.ledger.stamp(e, d.Faction, d.Round)
	n.faction(d.Faction).directed(d, n.at(d.Round), n.world.name)
}

// applyOrders records a faction's orders for their round. Orders for a
// round are issued in the round before, so they belong to it, but the
// ledger times them under their own round.
func (n *narrator) applyOrders(e event.Event, d orders) {
	n.belongs(d.Round - 1)
	n.ledger.stamp(e, d.Faction, d.Round)
	n.faction(d.Faction).ordered(d, n.world.resolves)
}

// applyConcluded takes the conclusion.
func (n *narrator) applyConcluded(d concluded) {
	n.end = &d
	n.concluded.fire()
}

// settle fires settled once the exercise has concluded, each faction's
// assessment of the concluded round is in, and, for each faction alerted to
// an objective lost in that round, a revision of that assessment is in.
// intelligence revises an assessment when an alert follows it, giving it a
// higher revision than the first it issued; a redelivery does not raise the
// revision.
func (n *narrator) settle() {
	if n.end == nil || n.world.setup == nil {
		return
	}
	for _, name := range n.world.setup.Factions {
		f := n.faction(name)
		if f.picture == nil || f.picture.Round < n.end.Round {
			return
		}
		if r := f.revisions[n.end.Round]; f.lostAt(n.end.Round) && r.highest <= r.first {
			return
		}
	}
	n.settled.fire()
}

// belongs notes an event of round, so the blocks before it are complete.
func (n *narrator) belongs(round int) { n.cursor.seen = max(n.cursor.seen, round) }

// advance narrates, once the initial conditions are, each block of a round
// before round, in order.
func (n *narrator) advance(round int) {
	if !n.cursor.told {
		return
	}
	for r := n.cursor.printed + 1; r < round; r++ {
		n.print(r)
	}
}

// begin opens the narration once its step has begun, and tells what the
// events handled before it hold. Until then the narrator only collects, so
// no line lands under the step that joins the stream.
func (n *narrator) begin() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.cursor.open = true
	n.tell()
	n.advance(n.cursor.seen)
}

// finish narrates what remains, under the narrator's lock.
func (n *narrator) finish() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.flush()
}

// flush narrates every block not yet narrated, then the late changes left.
// The caller holds mu.
func (n *narrator) flush() {
	if !n.cursor.told {
		return
	}
	n.advance(n.cursor.seen + 1)
	if late := n.late(); len(late) > 0 {
		n.note("")
		for _, l := range n.rows("  ", labeled{label: "late", items: late, each: true}) {
			n.note("%s", l)
		}
	}
}

// late returns, and forgets, the changes collected for rounds already
// narrated, each tagged with its round and side.
func (n *narrator) late() []string {
	var out []string
	for _, r := range slices.Sorted(maps.Keys(n.cursor.blocks)) {
		if r > n.cursor.printed {
			continue
		}
		for _, s := range n.sides(n.cursor.blocks[r]) {
			for _, l := range s.rows {
				out = append(out, fmt.Sprintf("%d %s %s %s", r, s.name, l.label, strings.Join(l.items, " · ")))
			}
		}
		delete(n.cursor.blocks, r)
	}
	return out
}

// print narrates the block of round, set off from the block before by a
// blank line. It includes the late changes collected since, or says that the
// round changed nothing.
func (n *narrator) print(round int) {
	late := n.late()
	b := n.at(round)
	delete(n.cursor.blocks, round)
	// The initial conditions end on a blank line.
	if n.cursor.printed >= 0 {
		n.note("")
	}
	n.cursor.printed = round
	sides := n.sides(b)
	if len(sides) == 0 && len(late) == 0 {
		n.note("round %d  no change", round)
		return
	}
	n.note("round %d", round)
	for _, s := range sides {
		n.note("  %s", s.name)
		for _, l := range s.rows {
			for _, line := range n.rows("    ", l) {
				n.note("%s", line)
			}
		}
	}
	if len(late) > 0 {
		for _, line := range n.rows("  ", labeled{label: "late", items: late, each: true}) {
			n.note("%s", line)
		}
	}
}
