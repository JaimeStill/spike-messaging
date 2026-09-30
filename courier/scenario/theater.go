package scenario

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"uuid"

	"github.com/spf13/pflag"

	"github.com/JaimeStill/spike-messaging/core/event"
	corelifecycle "github.com/JaimeStill/spike-messaging/core/lifecycle"
	"github.com/JaimeStill/spike-messaging/core/reactor"
	"github.com/JaimeStill/spike-messaging/messaging"
)

// TheaterDurable prefixes the durable consumer each theater run subscribes
// under, so the stream's consumers show which are courier's.
const TheaterDurable = "courier-theater-"

// The events the theater narrates beyond the assessments scenario's: the
// observer's record of each round, its alert to a faction that lost an
// objective, and operations' orders.
const (
	resolvedType = "exercise.round.resolved"
	lostType     = "exercise.objective.lost"
	ordersType   = "operations.orders.issued"
)

// captureRounds is how many rounds in a row a faction must end alone on an
// objective to take it, as exercise's rules set it.
const captureRounds = 2

// settle is how long the theater waits, after the conclusion, for the final
// round's assessments, which are issued after it.
const settle = 3 * time.Second

// The theater's own readings of the payloads it narrates, as far as it reads
// them: the services share no Go types. The narration calls an exercise
// element a squad, so the type for an element is squad.
type (
	theaterStarted struct {
		Exercise string `json:"exercise"`
		Name     string `json:"name"`
		Map      struct {
			Sectors []struct {
				ID     string `json:"id"`
				Width  int    `json:"width"`
				Height int    `json:"height"`
			} `json:"sectors"`
		} `json:"map"`
		Factions        []string `json:"factions"`
		RoundIntervalMS int64    `json:"round_interval_ms"`
		RoundLimit      int      `json:"round_limit"`
	}
	squad struct {
		ID       string   `json:"id"`
		Kind     string   `json:"kind"`
		Strength int      `json:"strength"`
		Health   []int    `json:"health"`
		Status   string   `json:"status"`
		At       location `json:"at"`
	}
	theaterObserved struct {
		Exercise string  `json:"exercise"`
		Faction  string  `json:"faction"`
		Round    int     `json:"round"`
		Own      []squad `json:"own"`
	}
	engaged struct {
		ID      string `json:"id"`
		Faction string `json:"faction"`
		Before  int    `json:"before"`
		After   int    `json:"after"`
		Fallen  int    `json:"fallen"`
	}
	resolvedData struct {
		Exercise string `json:"exercise"`
		Round    int    `json:"round"`
		Retreats []struct {
			ID      string   `json:"id"`
			Faction string   `json:"faction"`
			From    location `json:"from"`
			To      location `json:"to"`
			Before  int      `json:"before"`
			After   int      `json:"after"`
			Fallen  int      `json:"fallen"`
			// Pursuers are the enemy elements that fired on the retreat,
			// with their strength before and after its return fire.
			Pursuers []engaged `json:"pursuers"`
		} `json:"retreats"`
		Engagements []struct {
			At       location  `json:"at"`
			Elements []engaged `json:"elements"`
		} `json:"engagements"`
		Captures []struct {
			At      location `json:"at"`
			Faction string   `json:"faction"`
			From    string   `json:"from"`
		} `json:"captures"`
		Losses []struct {
			ID      string `json:"id"`
			Faction string `json:"faction"`
		} `json:"losses"`
		Progress []struct {
			At      location `json:"at"`
			Faction string   `json:"faction"`
			Rounds  int      `json:"rounds"`
		} `json:"progress"`
	}
	lostData struct {
		Exercise string   `json:"exercise"`
		Faction  string   `json:"faction"`
		Round    int      `json:"round"`
		At       location `json:"at"`
		Holder   string   `json:"holder"`
	}
	ordersData struct {
		Exercise string `json:"exercise"`
		Faction  string `json:"faction"`
		Round    int    `json:"round"`
		Orders   []struct {
			Element string     `json:"element"`
			Steps   []location `json:"steps"`
			Retreat bool       `json:"retreat"`
			Pursue  bool       `json:"pursue"`
		} `json:"orders"`
	}
)

// swing renders an element's strength through a fight or a retreat, with
// the operators it lost, or that it was destroyed.
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

// theaterScenario joins the exercise services' stream and narrates one
// exercise as a demonstration. It narrates the initial conditions, then
// each round as a block once the round is complete, by side: what the
// observer recorded, then what each faction knows, decides, and orders.
// It closes with the final conditions, with a ledger of the stream's
// traffic and the chain's latency.
func theaterScenario(joins Joins, needs func() []Need) Scenario {
	var exercise, exerciseURL, stream, prefix string
	maxAge := 24 * time.Hour
	wait := 2 * time.Minute
	return Scenario{
		Name:    "theater",
		Summary: "Narrate an exercise as the services play it: initial conditions, what each round changed, by side, and the final conditions",
		Needs:   needs,
		Flags: func(fs *pflag.FlagSet) {
			fs.StringVar(&exercise, "exercise", "", "the ID of the exercise to narrate, joined before it starts (required)")
			fs.StringVar(&exerciseURL, "exercise-url", "http://localhost:8081", "the exercise service's base URL, which tells the seed and the objectives")
			fs.StringVar(&stream, "stream", "exercise", "the stream the exercise services share")
			fs.StringVar(&prefix, "prefix", "exercise", "the subject prefix of the services' stream")
			fs.DurationVar(&maxAge, "max-age", maxAge, "the stream's max_age, as the services configure it: the last broker to provision the stream sets it")
			fs.DurationVar(&wait, "wait", wait, "how long to wait for the exercise to conclude")
		},
		Validate: func() error {
			var errs []error
			if _, err := uuid.Parse(exercise); err != nil {
				errs = append(errs, errors.New("--exercise must be an exercise's ID"))
			}
			if err := validExerciseURL(exerciseURL); err != nil {
				errs = append(errs, err)
			}
			if wait <= 0 {
				errs = append(errs, errors.New("--wait must be positive"))
			}
			return errors.Join(errs...)
		},
		Steps: func() ([]Step, func() error) {
			c := newCoordinator(defaultDrain)
			var l lease
			n := newNarrator(exercise)
			n.read = func(ctx context.Context) (exerciseView, error) { return readExercise(ctx, exerciseURL, exercise) }
			return []Step{
				{
					Intent: fmt.Sprintf("Join the %s stream, and wait for exercise %s to start", stream, exercise),
					Action: func(ctx context.Context, rep *Reporter) error {
						if joins == nil {
							return errors.New("no stream to join is configured")
						}
						b, release, err := joins(stream, prefix, maxAge)
						if err != nil {
							return err
						}
						l.release = release
						src, err := b.Subscribe(messaging.Subscription{
							Name: TheaterDurable + strings.ReplaceAll(uuid.NewV7().String(), "-", ""),
							Types: []string{
								startedType, resolvedType, lostType, observedType, assessmentType,
								directiveType, ordersType, concludedType,
							},
						})
						if err != nil {
							return err
						}
						n.note = rep.Note
						corelifecycle.Register(c.lc, "watch", 0, reactor.New(src, n.handle, reactor.Grace(defaultGrace)))
						if err := c.start(ctx, rep); err != nil {
							return err
						}
						wctx, cancel := context.WithTimeout(ctx, wait)
						defer cancel()
						select {
						case <-n.started.ch:
							return nil
						case <-wctx.Done():
							return fmt.Errorf("exercise %s did not start within %s", exercise, wait)
						}
					},
				},
				{
					Intent: "Narrate events until exercise completion",
					Action: func(ctx context.Context, _ *Reporter) error {
						wctx, cancel := context.WithTimeout(ctx, wait)
						defer cancel()
						select {
						case <-n.concluded.ch:
						case <-wctx.Done():
							return fmt.Errorf("exercise %s did not conclude within %s", exercise, wait)
						}
						// The final round's assessments, and the revisions of those
						// of a faction that lost an objective in it, follow the
						// conclusion.
						select {
						case <-n.settled.ch:
						case <-time.After(settle):
						case <-ctx.Done():
							return ctx.Err()
						}
						n.finish()
						return nil
					},
				},
				{
					Intent: "Skirmish complete, results",
					Action: func(_ context.Context, rep *Reporter) error {
						for _, line := range n.final() {
							rep.Note("%s", line)
						}
						return nil
					},
				},
				{
					Intent: "Signal the drain: the watch stops receiving",
					Action: func(_ context.Context, rep *Reporter) error { return c.stop(rep) },
				},
			}, afterDrain(c, &l)
		},
	}
}

// narrator follows one exercise's events and narrates them: the initial
// conditions once the start (when it reads the seed and the objectives from
// exercise's API) and round 0's observations are in, then each round as a
// block, and, on demand, the final conditions. It collects what each event
// changes in the block of the round it belongs to, and narrates a block
// once an event of a later round arrives, so a block tells a round's story
// in its order rather than the events'. A change that arrives for a round
// already narrated is told as late in the next block. It records each
// event's type and time for the ledger. Its methods are safe to call
// concurrently from the reactor's goroutine and the scenario's.
type narrator struct {
	exercise                    string
	note                        func(string, ...any)
	read                        func(context.Context) (exerciseView, error) // reads the exercise's view from its API
	started, concluded, settled *signal

	mu        sync.Mutex
	setup     *theaterStarted
	view      exerciseView                      // the observer's view, read at the start
	initial   map[string][]squad                // each faction's squads in round 0
	latest    map[string][]squad                // each faction's squads in its latest observation
	told      bool                              // whether the initial conditions were narrated
	blocks    map[int]*block                    // the rounds not yet narrated, and late changes
	seen      int                               // the latest round an event belongs to
	printed   int                               // the last round narrated, or -1
	sites     []location                        // the objectives, as the observer's view has them
	holders   map[string]string                 // each objective's holder, by place, as the captures tell it
	regroups  map[string]map[string]bool        // each faction's recovering squads already narrated
	losses    map[string]map[string]lostData    // each faction's objectives lost, by place
	pictures  map[string]assessmentData         // each faction's last assessment
	standing  standing                          // each faction's last directive, by element
	moving    map[string]string                 // the key of each faction's orders in effect, as narrated
	orders    map[string]map[int]movement       // each faction's latest orders not yet resolved, by round
	resolves  int                               // the last round resolved
	revisions map[string][2]int                 // the first and highest revision of each faction's assessments, by "faction/round"
	counts    map[string]int                    // events by type
	times     map[string]map[string][]time.Time // event times, by type and "faction/round"
	end       *concludedData
}

// block is what one round changed, as the narration tells it: the
// observer's record of the round, and what each faction knows, decides,
// and orders in it.
type block struct {
	observer []labeled                      // fights and retreats, one row each
	shown    map[string]bool                // the elements the observer's rows show destroyed
	results  map[string]map[string][]string // each faction's objective results, by verb
	fates    map[string][]string            // each faction's elements destroyed or regrouping
	knows    map[string][]string            // each faction's assessment changes
	decides  map[string][]detail            // each faction's directive changes, one per element
	orders   map[string]string              // each faction's orders, as issued in the round
}

// resultVerbs orders a faction's objective results in its row.
var resultVerbs = []string{"captures", "takes", "loses"}

func newNarrator(exercise string) *narrator {
	return &narrator{
		exercise:  exercise,
		note:      func(string, ...any) {},
		revisions: map[string][2]int{},
		read:      func(context.Context) (exerciseView, error) { return exerciseView{}, nil },
		started:   newSignal(), concluded: newSignal(), settled: newSignal(),
		initial: map[string][]squad{}, latest: map[string][]squad{},
		blocks: map[int]*block{}, printed: -1,
		holders: map[string]string{}, regroups: map[string]map[string]bool{}, losses: map[string]map[string]lostData{}, pictures: map[string]assessmentData{},
		standing: standing{}, moving: map[string]string{}, orders: map[string]map[int]movement{},
		counts: map[string]int{}, times: map[string]map[string][]time.Time{},
	}
}

// handle collects one event of the stream in the block of the round it
// belongs to, ignoring every other exercise's, and narrates the blocks it
// completes.
func (n *narrator) handle(ctx context.Context, e event.Event) error {
	var head struct {
		Exercise string `json:"exercise"`
	}
	if err := json.Unmarshal(e.Data, &head); err != nil || head.Exercise != n.exercise {
		return nil
	}
	// The objectives are read before any later event is handled, so every
	// cell is named as it is told.
	var view exerciseView
	if e.Type == startedType {
		var err error
		if view, err = n.read(ctx); err != nil {
			return fmt.Errorf("read exercise %s: %w", n.exercise, err)
		}
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.counts[e.Type]++
	var err error
	switch e.Type {
	case startedType:
		var d theaterStarted
		if err = json.Unmarshal(e.Data, &d); err == nil {
			n.setup = &d
			n.learn(view)
			n.started.fire()
		}
	case resolvedType:
		var d resolvedData
		if err = json.Unmarshal(e.Data, &d); err == nil {
			n.belongs(d.Round)
			n.resolved(d)
		}
	case lostType:
		var d lostData
		if err = json.Unmarshal(e.Data, &d); err == nil {
			n.belongs(d.Round)
			if n.losses[d.Faction] == nil {
				n.losses[d.Faction] = map[string]lostData{}
			}
			n.losses[d.Faction][place(d.At)] = d
			n.holders[place(d.At)] = d.Holder
			n.result(d.Round, d.Faction, "loses", n.name(d.At)+" to "+d.Holder)
		}
	case observedType:
		var d theaterObserved
		if err = json.Unmarshal(e.Data, &d); err == nil {
			n.belongs(d.Round)
			n.stamp(e, d.Faction, d.Round)
			n.latest[d.Faction] = d.Own
			n.regrouped(d)
			if d.Round == 0 {
				n.initial[d.Faction] = d.Own
			}
		}
	case assessmentType:
		var d assessmentData
		if err = json.Unmarshal(e.Data, &d); err == nil {
			n.belongs(d.Round)
			n.stamp(e, d.Faction, d.Round)
			n.assessed(d)
			k := d.Faction + "/" + strconv.Itoa(d.Round)
			if r, ok := n.revisions[k]; !ok {
				n.revisions[k] = [2]int{d.Revision, d.Revision}
			} else {
				n.revisions[k] = [2]int{r[0], max(r[1], d.Revision)}
			}
		}
	case directiveType:
		var d directiveData
		if err = json.Unmarshal(e.Data, &d); err == nil {
			n.belongs(d.Round)
			n.stamp(e, d.Faction, d.Round)
			n.directed(d)
		}
	case ordersType:
		var d ordersData
		if err = json.Unmarshal(e.Data, &d); err == nil {
			// Orders for a round are issued in the round before.
			n.belongs(d.Round - 1)
			n.stamp(e, d.Faction, d.Round)
			n.ordered(d)
		}
	case concludedType:
		var d concludedData
		if err = json.Unmarshal(e.Data, &d); err == nil {
			n.end = &d
			n.concluded.fire()
		}
	}
	if err != nil {
		return event.Permanent(err)
	}
	n.tell()
	n.advance(n.seen)
	n.settle()
	return nil
}

// cellOf returns a lookup of the cell of each of faction's squads in its
// latest observation.
func (n *narrator) cellOf(faction string) func(string) (location, bool) {
	return func(id string) (location, bool) {
		for _, s := range n.latest[faction] {
			if s.ID == id {
				return s.At, true
			}
		}
		return location{}, false
	}
}

// stamp records e's time under its type, its faction, and its round.
func (n *narrator) stamp(e event.Event, faction string, round int) {
	if n.times[e.Type] == nil {
		n.times[e.Type] = map[string][]time.Time{}
	}
	k := faction + "/" + fmt.Sprint(round)
	n.times[e.Type][k] = append(n.times[e.Type][k], e.Time)
}

// belongs notes an event of round, so the blocks before it are complete.
func (n *narrator) belongs(round int) { n.seen = max(n.seen, round) }

// at returns the block of round.
func (n *narrator) at(round int) *block {
	b := n.blocks[round]
	if b == nil {
		b = &block{
			shown: map[string]bool{}, results: map[string]map[string][]string{}, fates: map[string][]string{},
			knows: map[string][]string{}, decides: map[string][]detail{}, orders: map[string]string{},
		}
		n.blocks[round] = b
	}
	return b
}

// result records an objective result of a faction's in the block of round.
func (n *narrator) result(round int, faction, verb, what string) {
	b := n.at(round)
	if b.results[faction] == nil {
		b.results[faction] = map[string][]string{}
	}
	b.results[faction][verb] = append(b.results[faction][verb], what)
}

// advance narrates, once the initial conditions are, each block of a round
// before round, in order.
func (n *narrator) advance(round int) {
	if !n.told {
		return
	}
	for r := n.printed + 1; r < round; r++ {
		n.print(r)
	}
}

// finish narrates what remains, under the narrator's lock.
func (n *narrator) finish() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.flush()
}

// flush narrates every block not yet narrated, and the late changes left.
// The caller holds mu.
func (n *narrator) flush() {
	if !n.told {
		return
	}
	n.advance(n.seen + 1)
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
	for _, r := range slices.Sorted(maps.Keys(n.blocks)) {
		if r > n.printed {
			continue
		}
		for _, s := range n.sides(n.blocks[r]) {
			for _, l := range s.rows {
				out = append(out, fmt.Sprintf("%d %s %s %s", r, s.name, l.label, strings.Join(l.items, " · ")))
			}
		}
		delete(n.blocks, r)
	}
	return out
}

// print narrates the block of round, set off from the one before by a
// blank line, with the late changes collected since, or that the round
// changed nothing.
func (n *narrator) print(round int) {
	late := n.late()
	b := n.at(round)
	delete(n.blocks, round)
	// The initial conditions end on a blank line.
	if n.printed >= 0 {
		n.note("")
	}
	n.printed = round
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

// side is one side of a block: the observer, or a faction, with its rows.
type side struct {
	name string
	rows []labeled
}

// sides returns a block's sides that have rows: the observer's fights,
// retreats, and each faction's results, then each faction's knows,
// decides (by squad), and orders, in the exercise's order of factions.
func (n *narrator) sides(b *block) []side {
	factions := n.factions(b)
	observer := side{name: "observer", rows: slices.Clone(b.observer)}
	for _, f := range factions {
		var items []string
		for _, verb := range resultVerbs {
			if what := b.results[f][verb]; len(what) > 0 {
				items = append(items, verb+" "+strings.Join(what, " · "))
			}
		}
		items = append(items, b.fates[f]...)
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
			s.rows = append(s.rows, labeled{label: "decides", items: list(decides, "", same), each: true})
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

// factions returns the exercise's factions, then any other a block names,
// sorted.
func (n *narrator) factions(b *block) []string {
	var out []string
	if n.setup != nil {
		out = slices.Clone(n.setup.Factions)
	}
	var others []string
	for _, m := range []map[string]bool{
		keysOf(b.results), keysOf(b.fates), keysOf(b.knows), keysOf(b.decides), keysOf(b.orders),
	} {
		for f := range m {
			if !slices.Contains(out, f) && !slices.Contains(others, f) {
				others = append(others, f)
			}
		}
	}
	slices.Sort(others)
	return append(out, others...)
}

func keysOf[V any](m map[string]V) map[string]bool {
	out := make(map[string]bool, len(m))
	for k := range m {
		out[k] = true
	}
	return out
}

// labelWidth is the width to which a row's label is padded: the longest
// of the rows' own labels, and of the factions'.
func (n *narrator) labelWidth() int {
	w := len("retreat")
	if n.setup != nil {
		for _, f := range n.setup.Factions {
			w = max(w, len(f))
		}
	}
	return w
}

// rows renders a row at indent, its label padded to the rows' width.
func (n *narrator) rows(indent string, l labeled) []string {
	return l.lines(indent, n.labelWidth(), same)
}

// name renders a cell for the narration: an objective's as objective:x,y,
// any other as sector:x,y.
func (n *narrator) name(l location) string {
	p := place(l)
	if _, ok := n.holders[p]; ok {
		_, xy, _ := strings.Cut(p, ":")
		return "objective:" + xy
	}
	return p
}

// learn takes the seed and the objectives from the observer's view, read at
// the start, when no objective is held.
func (n *narrator) learn(v exerciseView) {
	n.view = v
	n.sites = v.State.objectives()
	for _, l := range n.sites {
		n.holders[place(l)] = ""
	}
}

// tell narrates the initial conditions once the start and each faction's
// round-0 observation are in.
func (n *narrator) tell() {
	if n.told || n.setup == nil {
		return
	}
	for _, f := range n.setup.Factions {
		if _, ok := n.initial[f]; !ok {
			return
		}
	}
	n.told = true
	s := n.setup
	var sizes []string
	for _, sec := range s.Map.Sectors {
		size := fmt.Sprintf("%dx%d", sec.Width, sec.Height)
		if len(s.Map.Sectors) > 1 {
			size = sec.ID + " " + size
		}
		sizes = append(sizes, size)
	}
	objectives := row(col("key", "objectives"), apart("value", "none"))
	if len(n.sites) > 0 {
		objectives = row(col("key", "objectives"), apart("value", "hidden from both factions"))
		for _, l := range n.sites {
			objectives.sub = append(objectives.sub, text(n.name(l)))
		}
	}
	conditions := []detail{
		row(col("key", "map size"), apart("value", strings.Join(sizes, ", "))),
		row(col("key", "seed"), apart("value", strconv.FormatInt(n.view.Seed, 10))),
		objectives,
	}
	for _, f := range s.Factions {
		conditions = append(conditions, row(col("key", f)).with(squadRows(n.initial[f], true)...))
	}
	n.note("%s: %d rounds at %s", s.Name, s.RoundLimit, time.Duration(s.RoundIntervalMS)*time.Millisecond)
	for _, l := range list(conditions, "  ", same) {
		n.note("%s", l)
	}
	n.note("")
}

// settle fires settled once the exercise has concluded, each faction's
// assessment of the concluded round is in, and a revision of it is in for
// each faction alerted to an objective lost in that round: intelligence
// revises an assessment when an alert follows it, with a higher revision
// than the first it issued, which a redelivery does not raise.
func (n *narrator) settle() {
	if n.end == nil || n.setup == nil {
		return
	}
	for _, f := range n.setup.Factions {
		if p, ok := n.pictures[f]; !ok || p.Round < n.end.Round {
			return
		}
		if r := n.revisions[f+"/"+strconv.Itoa(n.end.Round)]; n.lostAt(f, n.end.Round) && r[1] <= r[0] {
			return
		}
	}
	n.settled.fire()
}

// lostAt reports whether a faction was alerted to an objective lost in round.
func (n *narrator) lostAt(faction string, round int) bool {
	for _, l := range n.losses[faction] {
		if l.Round == round {
			return true
		}
	}
	return false
}

// resolved collects the observer's record of a round: the orders each
// faction had in effect for it, in the block of the round before, where
// they were issued, then each fight, each retreat, each squad destroyed,
// each objective a faction is taking, and each that changed hands.
func (n *narrator) resolved(d resolvedData) {
	n.resolves = d.Round
	for _, c := range d.Captures {
		n.holders[place(c.At)] = c.Faction
	}
	if n.setup != nil {
		for _, f := range n.setup.Factions {
			n.inEffect(f, d.Round)
		}
	}
	b := n.at(d.Round)
	show := func(e engaged) string {
		if e.After == 0 {
			b.shown[e.ID] = true
		}
		return swing(e)
	}
	for _, g := range d.Engagements {
		items := []string{n.name(g.At)}
		for _, e := range g.Elements {
			items = append(items, show(e))
		}
		b.observer = append(b.observer, labeled{label: "fight", items: []string{strings.Join(items, "  ")}})
	}
	for _, t := range d.Retreats {
		what := t.ID + " " + n.name(t.From) + " -> " + n.name(t.To)
		if len(t.Pursuers) == 0 {
			what += " unpursued"
		} else {
			fire := []string{show(engaged{ID: t.ID, Faction: t.Faction, Before: t.Before, After: t.After, Fallen: t.Fallen})}
			for _, p := range t.Pursuers {
				fire = append(fire, show(p))
			}
			what += " pursued  " + strings.Join(fire, ", ")
		}
		b.observer = append(b.observer, labeled{label: "retreat", items: []string{what}})
	}
	for _, c := range d.Captures {
		what := n.name(c.At)
		if c.From != "" {
			what += " from " + c.From
		}
		n.result(d.Round, c.Faction, "captures", what)
	}
	for _, a := range d.Progress {
		n.result(d.Round, a.Faction, "takes", fmt.Sprintf("%s %d/%d", n.name(a.At), a.Rounds, captureRounds))
	}
	for _, l := range d.Losses {
		if !b.shown[l.ID] {
			b.fates[l.Faction] = append(b.fates[l.Faction], l.ID+" destroyed")
		}
	}
}

// regrouped collects each own element an observation shows recovering,
// once for each time it retreats: it sits out the round after the observed
// one.
func (n *narrator) regrouped(d theaterObserved) {
	was := n.regroups[d.Faction]
	now := map[string]bool{}
	for _, s := range d.Own {
		if s.Status == "recovering" {
			now[s.ID] = true
		}
	}
	for _, id := range slices.Sorted(maps.Keys(now)) {
		if !was[id] {
			b := n.at(d.Round)
			b.fates[d.Faction] = append(b.fates[d.Faction], fmt.Sprintf("%s regroups, sits out %d", id, d.Round+1))
		}
	}
	n.regroups[d.Faction] = now
}

// assessed collects what a faction's assessment changed in what it knows:
// contacts it spots, contacts it loses track of, and objectives it sees
// held differently, or learns of from a loss alert. intelligence revises a
// round's assessment when an alert follows it, so a round may be assessed
// twice; the second adds only what the alert changed.
func (n *narrator) assessed(d assessmentData) {
	prev, had := n.pictures[d.Faction]
	n.pictures[d.Faction] = d
	was := map[string]assessedContact{}
	for _, c := range prev.Contacts {
		was[c.ID] = c
	}
	var changes []string
	for _, c := range d.Contacts {
		if p, ok := was[c.ID]; c.Age == 0 && (!ok || p.Age > 0) {
			changes = append(changes, fmt.Sprintf("spotted %s %d @ %s", c.ID, c.Strength, place(c.At)))
		}
	}
	now := ids(d.Contacts, func(c assessedContact) string { return c.ID })
	for _, c := range prev.Contacts {
		if !now[c.ID] {
			changes = append(changes, "lost "+c.ID)
		}
	}
	believed := map[string]assessedObjective{}
	for _, o := range prev.Objectives {
		believed[place(o.At)] = o
	}
	for _, o := range d.Objectives {
		p := believed[place(o.At)]
		if !o.Known || had && p.Known && p.Holder == o.Holder {
			continue
		}
		state := heldBy(o.Holder)
		if l, ok := n.losses[d.Faction][place(o.At)]; ok && l.Round == o.Seen && l.Holder == o.Holder {
			state = "lost to " + o.Holder
		}
		changes = append(changes, n.name(o.At)+" "+state)
	}
	if len(changes) > 0 {
		b := n.at(d.Round)
		b.knows[d.Faction] = append(b.knows[d.Faction], changes...)
	}
}

// directed collects each squad whose directive a faction's directives
// change. A squad decided on again in the same round shows only its latest
// directive.
func (n *narrator) directed(d directiveData) {
	for _, x := range n.standing.changes(d) {
		r := row(col("id", x.Element))
		verb, target, contact := act(x, n.cellOf(d.Faction), n.name)
		r.cells = append(r.cells, col("verb", verb))
		if target != "" {
			r.cells = append(r.cells, col("target", target))
		}
		if contact != "" {
			r.cells = append(r.cells, col("contact", contact))
		}
		b := n.at(d.Round)
		rows := slices.DeleteFunc(b.decides[d.Faction], func(p detail) bool { return p.cells[0].text == x.Element })
		b.decides[d.Faction] = append(rows, r)
	}
}

// movement is the squads a faction's orders move, by how they move.
type movement struct {
	move, retreat, pursue []string
}

// key identifies a movement, to tell one from another.
func (m movement) key() string { return fmt.Sprint(m.move, m.retreat, m.pursue) }

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

// ordered records a faction's orders for their round. operations issues a
// round's orders when it sees the round before, and issues them again when
// a directive changes them. exercise keeps the last orders it records, so
// the theater narrates a round's orders when the round resolves, as the
// orders in effect. It does not narrate orders for a round already
// resolved, which exercise refuses.
func (n *narrator) ordered(d ordersData) {
	if d.Round <= n.resolves {
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
	if n.orders[d.Faction] == nil {
		n.orders[d.Faction] = map[int]movement{}
	}
	n.orders[d.Faction][d.Round] = m
}

// inEffect collects the orders a faction had in effect for round, in the
// block of the round before, where it issued them, when the squads they
// move, and how, differ from its last narrated orders, and forgets its
// orders for round and earlier.
func (n *narrator) inEffect(faction string, round int) {
	pending := n.orders[faction]
	m, ok := pending[round]
	for r := range pending {
		if r <= round {
			delete(pending, r)
		}
	}
	if !ok {
		return
	}
	if prev, told := n.moving[faction]; told && prev == m.key() {
		return
	}
	n.moving[faction] = m.key()
	n.at(round - 1).orders[faction] = m.String()
}

// final returns the final conditions: the verdict, who holds each
// objective, each faction's surviving squads and losses, the events on the
// stream by type, the chain's latency, and how to check the run. It first
// narrates the group still open.
func (n *narrator) final() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.flush()
	var out []detail
	if n.end != nil {
		out = append(out, text("verdict  "+verdict(*n.end)))
	}
	if n.setup != nil {
		objectives := text("objectives")
		if len(n.sites) == 0 {
			objectives = text("objectives  none")
		}
		for _, l := range n.sites {
			objectives = objectives.with(row(col("objective", n.name(l)), apart("holder", orUnheld(n.holders[place(l)]))))
		}
		out = append(out, objectives)
		for _, f := range n.setup.Factions {
			total := 0
			for _, s := range n.latest[f] {
				total += s.Strength
			}
			head := fmt.Sprintf("%s  strength %d", f, total)
			alive := ids(n.latest[f], func(s squad) string { return s.ID })
			var lost []string
			for _, s := range n.initial[f] {
				if !alive[s.ID] {
					lost = append(lost, s.ID)
				}
			}
			if len(lost) > 0 {
				head += ", lost " + strings.Join(lost, " ")
			}
			out = append(out, text(head).with(squadRows(n.latest[f], false)...))
		}
	}
	total := 0
	var kinds []detail
	for _, t := range []string{startedType, resolvedType, lostType, observedType, assessmentType, directiveType, ordersType, concludedType} {
		total += n.counts[t]
		kinds = append(kinds, row(col("type", t), part{key: "count", text: strconv.Itoa(n.counts[t]), apart: true, right: true}))
	}
	out = append(out, text(fmt.Sprintf("events  %d", total)).with(kinds...))
	out = append(out, text("chain p50").with(
		row(col("hop", "observed -> assessed"), apart("p50", p50(n.hops(observedType, assessmentType, 0)))),
		row(col("hop", "assessed -> directed"), apart("p50", p50(n.hops(assessmentType, directiveType, 0))+" (rounds with a directive)")),
		row(col("hop", "directed -> ordered"), apart("p50", p50(n.hops(directiveType, ordersType, 1)))),
	))
	out = append(out, text("check  mise run demo-theater-check "+n.exercise))
	return list(out, "", same)
}

// hops returns, for each faction and round with an event of type from, the
// time from it to the first later event of type to for the same faction on
// the round shift rounds on. A round with no event of type to counts for
// nothing, so assessed -> directed measures only rounds that led to a
// directive. A round's revised assessment follows its first, so
// observed -> assessed measures the first, and the revision counts toward
// assessed -> directed only when a directive follows it.
func (n *narrator) hops(from, to string, shift int) []time.Duration {
	var out []time.Duration
	for k, starts := range n.times[from] {
		faction, round, _ := strings.Cut(k, "/")
		var r int
		if _, err := fmt.Sscan(round, &r); err != nil {
			continue
		}
		ends := n.times[to][faction+"/"+fmt.Sprint(r+shift)]
		for _, s := range starts {
			var best time.Duration = -1
			for _, e := range ends {
				if d := e.Sub(s); d >= 0 && (best < 0 || d < best) {
					best = d
				}
			}
			if best >= 0 {
				out = append(out, best)
			}
		}
	}
	return out
}

// p50 renders the median of ds, or "none" when it is empty.
func p50(ds []time.Duration) string {
	if len(ds) == 0 {
		return "none"
	}
	slices.Sort(ds)
	return ds[len(ds)/2].Round(time.Millisecond).String()
}

// verdict renders how the exercise ended.
func verdict(c concludedData) string {
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

// squadRows renders a faction's squads by ID, with their kind and the
// health of their operators, and their cell when at is set.
func squadRows(ss []squad, at bool) []detail {
	ss = slices.Clone(ss)
	slices.SortFunc(ss, func(a, b squad) int { return cmp.Compare(a.ID, b.ID) })
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
// operators has health H, or each operator's health.
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

// standing is each faction's last directive, by element, so a new directive
// is narrated as what it changes.
type standing map[string]map[string]directive

// changes returns each directive of d that does not continue its element's
// last one, and records d as the faction's standing directives.
func (s standing) changes(d directiveData) []directive {
	was := s[d.Faction]
	now := make(map[string]directive, len(d.Directives))
	var out []directive
	for _, x := range d.Directives {
		now[x.Element] = x
		if prev, ok := was[x.Element]; ok && sameDirective(prev, x) {
			continue
		}
		out = append(out, x)
	}
	s[d.Faction] = now
	return out
}

// sameDirective reports whether b continues a: the same rule and contact,
// and the same target, except that an engage pursuing its contact to
// another cell continues it.
func sameDirective(a, b directive) bool {
	if a.Rule != b.Rule || a.Contact != b.Contact || (a.Target == nil) != (b.Target == nil) {
		return false
	}
	return a.Target == nil || a.Rule == "engage" && a.Contact != "" || *a.Target == *b.Target
}

// act renders what a directive has its squad do: a verb, the cell it heads
// for ("-> a:5,5") or fights in ("@ a:5,5"), and the contact it engages,
// each empty when the directive has none. The narration reads command's
// secure rule as a capture, and an engage in the squad's own cell, from
// cell (nil when unknown), as a fight in place. A directive without a rule,
// as courier's stand-in issues, heads for its target, or holds when it has
// none. name renders a cell.
func act(x directive, cell func(string) (location, bool), name func(location) string) (verb, target, contact string) {
	if x.Target == nil {
		return "hold", "", ""
	}
	to, at := "-> "+name(*x.Target), "@ "+name(*x.Target)
	switch {
	case x.Rule == "retreat":
		return "retreat", to, ""
	case x.Rule == "reinforce":
		return "reinforce", to, ""
	case x.Rule == "pursue":
		return "pursue", at, x.Contact
	case x.Rule == "engage" && ownCell(x, cell):
		return "fight", at, x.Contact
	case x.Rule == "engage":
		return "engage", to, x.Contact
	case x.Rule == "secure":
		return "capture", to, ""
	case x.Rule == "search":
		return "search", to, ""
	case x.Rule == "rescout":
		return "rescout", to, ""
	}
	return "head", to, ""
}

// ownCell reports whether x targets the cell its element stands in.
func ownCell(x directive, cell func(string) (location, bool)) bool {
	if cell == nil {
		return false
	}
	at, ok := cell(x.Element)
	return ok && at == *x.Target
}

// ids returns the set of IDs of xs.
func ids[T any](xs []T, id func(T) string) map[string]bool {
	out := make(map[string]bool, len(xs))
	for _, x := range xs {
		out[id(x)] = true
	}
	return out
}
