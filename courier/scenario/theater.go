package scenario

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
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
// umpire's record of each round, and operations' orders.
const (
	resolvedType = "exercise.round.resolved"
	ordersType   = "operations.orders.issued"
)

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
				ID         string  `json:"id"`
				Width      int     `json:"width"`
				Height     int     `json:"height"`
				Objectives []point `json:"objectives"`
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
	}
	resolvedData struct {
		Exercise    string `json:"exercise"`
		Round       int    `json:"round"`
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
	}
	ordersData struct {
		Exercise string `json:"exercise"`
		Faction  string `json:"faction"`
		Round    int    `json:"round"`
		Orders   []struct {
			Element string     `json:"element"`
			Steps   []location `json:"steps"`
		} `json:"orders"`
	}
)

// theaterScenario joins the exercise services' stream and narrates one
// exercise as a demonstration. It narrates the initial conditions, then one
// line for each event that changes something, as the services issue them,
// then the final conditions, with a ledger of the stream's traffic and the
// chain's latency.
func theaterScenario(joins Joins, needs func() []Need) Scenario {
	var exercise, stream, prefix string
	maxAge := 24 * time.Hour
	wait := 2 * time.Minute
	return Scenario{
		Name:    "theater",
		Summary: "Narrate an exercise as the services play it: initial conditions, each event that changes something, and the final conditions",
		Needs:   needs,
		Flags: func(fs *pflag.FlagSet) {
			fs.StringVar(&exercise, "exercise", "", "the ID of the exercise to narrate, joined before it starts (required)")
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
			if wait <= 0 {
				errs = append(errs, errors.New("--wait must be positive"))
			}
			return errors.Join(errs...)
		},
		Steps: func() ([]Step, func() error) {
			c := newCoordinator(defaultDrain)
			var l lease
			n := newNarrator(exercise)
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
								startedType, resolvedType, observedType, assessmentType,
								directiveType, ordersType, concludedType,
							},
						})
						if err != nil {
							return err
						}
						n.note = rep.Note
						handle := func(_ context.Context, e event.Event) error { return n.handle(e) }
						corelifecycle.Register(c.lc, "watch", 0, reactor.New(src, handle, reactor.Grace(defaultGrace)))
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
					Intent: "Narrate each event that changes something, until the exercise concludes",
					Action: func(ctx context.Context, _ *Reporter) error {
						wctx, cancel := context.WithTimeout(ctx, wait)
						defer cancel()
						select {
						case <-n.concluded.ch:
						case <-wctx.Done():
							return fmt.Errorf("exercise %s did not conclude within %s", exercise, wait)
						}
						// The final round's assessments follow the conclusion.
						select {
						case <-n.settled.ch:
						case <-time.After(settle):
						case <-ctx.Done():
							return ctx.Err()
						}
						return nil
					},
				},
				{
					Intent: "State the final conditions, and the traffic that carried the exercise",
					Action: func(_ context.Context, rep *Reporter) error {
						for _, line := range n.final() {
							rep.Note("%s", line)
						}
						rep.Note("check it against the umpire: mise run demo-theater-check %s", exercise)
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
// conditions once the start and round 0's observations are in, then one
// line for each event that changes something, and, on demand, the final
// conditions. It records each event's type and time for the ledger. Its
// methods are safe to call concurrently from the reactor's goroutine and
// the scenario's.
type narrator struct {
	exercise                    string
	note                        func(string, ...any)
	started, concluded, settled *signal

	mu       sync.Mutex
	setup    *theaterStarted
	initial  map[string][]squad                // each faction's squads in round 0
	latest   map[string][]squad                // each faction's squads in its latest observation
	told     bool                              // whether the initial conditions were narrated
	held     []string                          // lines that wait for the initial conditions
	holders  map[string]string                 // each objective's holder, by place
	pictures map[string]assessmentData         // each faction's last assessment
	standing standing                          // each faction's last directive, by element
	moving   map[string]string                 // each faction's moving squads, as narrated
	orders   map[string]map[int]string         // each faction's latest orders not yet resolved, by round
	resolves int                               // the last round resolved
	counts   map[string]int                    // events by type
	times    map[string]map[string][]time.Time // event times, by type and "faction/round"
	end      *concludedData
}

func newNarrator(exercise string) *narrator {
	return &narrator{
		exercise: exercise,
		note:     func(string, ...any) {},
		started:  newSignal(), concluded: newSignal(), settled: newSignal(),
		initial: map[string][]squad{}, latest: map[string][]squad{},
		holders: map[string]string{}, pictures: map[string]assessmentData{},
		standing: standing{}, moving: map[string]string{}, orders: map[string]map[int]string{},
		counts: map[string]int{}, times: map[string]map[string][]time.Time{},
	}
}

// handle narrates one event of the stream, ignoring every other exercise's.
func (n *narrator) handle(e event.Event) error {
	var head struct {
		Exercise string `json:"exercise"`
	}
	if err := json.Unmarshal(e.Data, &head); err != nil || head.Exercise != n.exercise {
		return nil
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
			n.started.fire()
		}
	case resolvedType:
		var d resolvedData
		if err = json.Unmarshal(e.Data, &d); err == nil {
			n.resolved(d)
		}
	case observedType:
		var d theaterObserved
		if err = json.Unmarshal(e.Data, &d); err == nil {
			n.stamp(e, d.Faction, d.Round)
			n.latest[d.Faction] = d.Own
			if d.Round == 0 {
				n.initial[d.Faction] = d.Own
			}
		}
	case assessmentType:
		var d assessmentData
		if err = json.Unmarshal(e.Data, &d); err == nil {
			n.stamp(e, d.Faction, d.Round)
			n.assessed(d)
		}
	case directiveType:
		var d directiveData
		if err = json.Unmarshal(e.Data, &d); err == nil {
			n.stamp(e, d.Faction, d.Round)
			if changes := n.standing.changes(d); len(changes) > 0 {
				n.line(d.Round, "command", d.Faction, strings.Join(changes, " · "))
			}
		}
	case ordersType:
		var d ordersData
		if err = json.Unmarshal(e.Data, &d); err == nil {
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
	n.settle()
	return nil
}

// stamp records e's time under its type, its faction, and its round.
func (n *narrator) stamp(e event.Event, faction string, round int) {
	if n.times[e.Type] == nil {
		n.times[e.Type] = map[string][]time.Time{}
	}
	k := faction + "/" + fmt.Sprint(round)
	n.times[e.Type][k] = append(n.times[e.Type][k], e.Time)
}

// line narrates one event, or holds it until the initial conditions are
// narrated.
func (n *narrator) line(round int, service, faction, what string) {
	l := fmt.Sprintf("r%-3d %-12s %-5s %s", round, service, faction, what)
	if !n.told {
		n.held = append(n.held, l)
		return
	}
	n.note("%s", l)
}

// tell narrates the initial conditions once the start and each faction's
// round-0 observation are in, then the lines held for them.
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
	var sectors, objectives []string
	for _, sec := range s.Map.Sectors {
		sectors = append(sectors, fmt.Sprintf("%s %d×%d", sec.ID, sec.Width, sec.Height))
		for _, p := range sec.Objectives {
			objectives = append(objectives, place(location{Sector: sec.ID, point: p}))
		}
	}
	n.note("%s: %d rounds at %s", s.Name, s.RoundLimit, time.Duration(s.RoundIntervalMS)*time.Millisecond)
	n.note("  map         %s", strings.Join(sectors, ", "))
	n.note("  objectives  %s", strings.Join(objectives, " · "))
	for _, f := range s.Factions {
		n.note("  %-11s %s", f, squads(n.initial[f], true))
	}
	n.note("")
	for _, l := range n.held {
		n.note("%s", l)
	}
	n.held = nil
}

// settle fires settled once the exercise has concluded and each faction's
// assessment of the concluded round is in.
func (n *narrator) settle() {
	if n.end == nil || n.setup == nil {
		return
	}
	for _, f := range n.setup.Factions {
		if p, ok := n.pictures[f]; !ok || p.Round < n.end.Round {
			return
		}
	}
	n.settled.fire()
}

// resolved narrates the umpire's record of a round: the orders each faction
// had in effect for it, then each fight, each squad destroyed, and each
// objective that changed hands.
func (n *narrator) resolved(d resolvedData) {
	n.resolves = d.Round
	if n.setup != nil {
		for _, f := range n.setup.Factions {
			n.inEffect(f, d.Round)
		}
	}
	for _, g := range d.Engagements {
		var sides []string
		for _, e := range g.Elements {
			sides = append(sides, fmt.Sprintf("%s %d→%d", e.ID, e.Before, e.After))
		}
		n.line(d.Round, "exercise", "", fmt.Sprintf("fight at %s: %s", place(g.At), strings.Join(sides, " · ")))
	}
	for _, l := range d.Losses {
		n.line(d.Round, "exercise", l.Faction, l.ID+" destroyed")
	}
	for _, c := range d.Captures {
		what := "captures " + place(c.At)
		if c.From != "" {
			what += " from " + c.From
		}
		n.holders[place(c.At)] = c.Faction
		n.line(d.Round, "exercise", c.Faction, what)
	}
}

// assessed narrates what a faction's assessment changed in what it knows:
// contacts it spots, contacts it loses track of, and objectives it sees
// held differently.
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
			changes = append(changes, fmt.Sprintf("spots %s (%d) at %s", c.ID, c.Strength, place(c.At)))
		}
	}
	now := ids(d.Contacts, func(c assessedContact) string { return c.ID })
	for _, c := range prev.Contacts {
		if !now[c.ID] {
			changes = append(changes, "loses track of "+c.ID)
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
		switch o.Holder {
		case "":
			changes = append(changes, "sees "+place(o.At)+" unheld")
		case d.Faction:
			changes = append(changes, "sees "+place(o.At)+" held")
		default:
			changes = append(changes, "sees "+place(o.At)+" held by "+o.Holder)
		}
	}
	if len(changes) > 0 {
		n.line(d.Round, "intelligence", d.Faction, strings.Join(changes, " · "))
	}
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
	var moving []string
	for _, o := range d.Orders {
		if len(o.Steps) > 0 {
			moving = append(moving, o.Element)
		}
	}
	slices.Sort(moving)
	if n.orders[d.Faction] == nil {
		n.orders[d.Faction] = map[int]string{}
	}
	n.orders[d.Faction][d.Round] = strings.Join(moving, " ")
}

// inEffect narrates the orders a faction had in effect for round when the
// squads they move differ from those its last narrated orders moved, and
// forgets its orders for round and earlier.
func (n *narrator) inEffect(faction string, round int) {
	pending := n.orders[faction]
	key, ok := pending[round]
	for r := range pending {
		if r <= round {
			delete(pending, r)
		}
	}
	if !ok {
		return
	}
	if prev, told := n.moving[faction]; told && prev == key {
		return
	}
	n.moving[faction] = key
	what := "all squads hold"
	if key != "" {
		what = "moves " + key
	}
	n.line(round, "operations", faction, what)
}

// final returns the final conditions: the verdict, who holds each
// objective, each faction's surviving squads and losses, the events on the
// stream by type, and the chain's latency.
func (n *narrator) final() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	var out []string
	if n.end != nil {
		out = append(out, "verdict     "+verdict(*n.end))
	}
	if n.setup != nil {
		var holds []string
		for _, sec := range n.setup.Map.Sectors {
			for _, p := range sec.Objectives {
				at := place(location{Sector: sec.ID, point: p})
				holder := n.holders[at]
				if holder == "" {
					holder = "unheld"
				}
				holds = append(holds, at+" "+holder)
			}
		}
		out = append(out, "objectives  "+strings.Join(holds, " · "))
		for _, f := range n.setup.Factions {
			line := fmt.Sprintf("%-11s %s", f, squads(n.latest[f], false))
			alive := ids(n.latest[f], func(s squad) string { return s.ID })
			var lost []string
			for _, s := range n.initial[f] {
				if !alive[s.ID] {
					lost = append(lost, s.ID)
				}
			}
			if len(lost) > 0 {
				line += " · lost " + strings.Join(lost, " ")
			}
			out = append(out, line)
		}
	}
	total := 0
	var kinds []string
	for _, t := range []string{startedType, resolvedType, observedType, assessmentType, directiveType, ordersType, concludedType} {
		total += n.counts[t]
		kinds = append(kinds, fmt.Sprintf("%d %s", n.counts[t], t))
	}
	out = append(out, fmt.Sprintf("events      %d on the stream: %s", total, strings.Join(kinds, " · ")))
	out = append(out, "chain p50   "+strings.Join([]string{
		"observed→assessed " + p50(n.hops(observedType, assessmentType, 0)),
		"assessed→directed " + p50(n.hops(assessmentType, directiveType, 0)) + " (rounds with a directive)",
		"directed→ordered " + p50(n.hops(directiveType, ordersType, 1)),
	}, " · "))
	return out
}

// hops returns, for each faction and round with an event of type from, the
// time from it to the first later event of type to for the same faction on
// the round shift rounds on. A round with no event of type to counts for
// nothing, so assessed→directed measures only rounds that led to a
// directive.
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

// squads renders a faction's squads by ID, with their kind and cell when
// full is set, and their total strength.
func squads(ss []squad, full bool) string {
	ss = slices.Clone(ss)
	slices.SortFunc(ss, func(a, b squad) int { return cmp.Compare(a.ID, b.ID) })
	var parts []string
	total := 0
	for _, s := range ss {
		total += s.Strength
		if full {
			parts = append(parts, fmt.Sprintf("%s %s %d at %s", s.ID, s.Kind, s.Strength, place(s.At)))
		} else {
			parts = append(parts, fmt.Sprintf("%s %d", s.ID, s.Strength))
		}
	}
	return fmt.Sprintf("%s · strength %d", orNone(parts, " · "), total)
}

// standing is each faction's last directive, by element, so a new directive
// is narrated as what it changes.
type standing map[string]map[string]directive

// changes renders each directive of d that does not continue its element's
// last one, and records d as the faction's standing directives. Each line
// names what the squad now does and, when it was doing something else, what
// that was.
func (s standing) changes(d directiveData) []string {
	was := s[d.Faction]
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
	s[d.Faction] = now
	return lines
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

// doing renders what a directive has its squad do, in the present tense
// ("engages b2 at a:5,5") or, when was is set, as a participle ("engaging
// b2 at a:5,5"). The narration reads command's secure rule as a capture. A
// directive without a rule, as courier's stand-in issues, heads for its
// target, or holds when it has none.
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
		return verb("captures ", "capturing ") + place(*x.Target)
	}
	return verb("heads for ", "heading for ") + place(*x.Target)
}

// ids returns the set of IDs of xs.
func ids[T any](xs []T, id func(T) string) map[string]bool {
	out := make(map[string]bool, len(xs))
	for _, x := range xs {
		out[id(x)] = true
	}
	return out
}
