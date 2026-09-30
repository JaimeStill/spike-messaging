package scenario

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/pflag"

	"github.com/JaimeStill/spike-messaging/core/event"
)

// TheaterCheckDurable prefixes the durable consumer each theater-check run
// subscribes under. Each run's durable is new, so it reads the stream from
// its beginning.
const TheaterCheckDurable = "courier-theater-check-"

// observerRound is one round of exercise's history: the state after it,
// each faction's observation of it, indexed like the factions, and its
// resolution, nil for round 0.
type observerRound struct {
	Round        int           `json:"round"`
	State        exerciseState `json:"state"`
	Observations []observation `json:"observations"`
	Resolution   *resolution   `json:"resolution"`
}

// theaterCheckScenario reconciles a theater run's assessments against
// exercise's history, the observer's record. It reads the assessments from
// the stream, from its beginning, and the history from exercise's API, and
// fails on any assessment the suppression rules do not explain.
func theaterCheckScenario(joins Joins, needs func() []Need) Scenario {
	j := newJoinFlags(time.Minute)
	var exerciseURL string
	// contactRounds is intelligence's contact_rounds config, which no
	// service's API tells, so courier takes it as a flag.
	contactRounds := 3
	idle := 2 * time.Second
	return Scenario{
		Name:    "theater-check",
		Summary: "Reconcile an exercise's assessments against the observer's record, and state what each faction believes at the end",
		Needs:   needs,
		Flags: func(fs *pflag.FlagSet) {
			j.bind(fs, "the ID of the exercise to check (required)", "how long to wait for the exercise's conclusion and the assessments after it")
			fs.StringVar(&exerciseURL, "exercise-url", "http://localhost:8081", "the exercise service's base URL")
			fs.IntVar(&contactRounds, "contact-rounds", contactRounds, "the rounds intelligence remembers a contact: intelligence's contact_rounds config, which courier cannot read")
			fs.DurationVar(&idle, "idle", idle, "how long the stream must be quiet after the conclusion before the assessments are complete")
		},
		Validate: func() error {
			// --idle is a wait too, so the check words the two as one rule.
			errs := []error{j.exerciseErr(), validExerciseURL(exerciseURL)}
			if contactRounds < 0 {
				errs = append(errs, errors.New("--contact-rounds must not be negative"))
			}
			if j.wait <= 0 || idle <= 0 {
				errs = append(errs, errors.New("--wait and --idle must be positive"))
			}
			return errors.Join(errs...)
		},
		Steps: func() ([]Step, func() error) {
			c := newCoordinator(defaultDrain)
			var l lease
			g := &assessmentLog{exercise: j.exercise, concluded: newSignal()}
			var history []observerRound
			var end *verdict
			var rules ruleset
			return []Step{
				{
					Intent: fmt.Sprintf("Join the %s stream from its beginning, and read exercise %s's assessments until it concludes and the stream is quiet", j.stream, j.exercise),
					Action: func(ctx context.Context, rep *Reporter) error {
						if _, err := l.join(ctx, rep, c, joins, j, TheaterCheckDurable, g.handle, assessmentType, concludedType); err != nil {
							return err
						}
						wctx, cancel := context.WithTimeout(ctx, j.wait)
						defer cancel()
						select {
						case <-g.concluded.ch:
						case <-wctx.Done():
							return fmt.Errorf("exercise %s did not conclude within %s", j.exercise, j.wait)
						}
						// The final round's assessments, and any revision, follow
						// the conclusion.
						for quiet := idle - g.since(); quiet > 0; quiet = idle - g.since() {
							if !sleep(wctx, quiet) {
								rep.Note("the stream was not quiet for %s within %s", idle, j.wait)
								break
							}
						}
						rep.Note("read %d assessments", len(g.read()))
						return nil
					},
				},
				stopWatch(c),
				{
					Intent: fmt.Sprintf("Read the observer's record of exercise %s from %s", j.exercise, exerciseURL),
					Action: func(ctx context.Context, rep *Reporter) error {
						if err := getJSON(ctx, exerciseEndpoint(exerciseURL, j.exercise)+"/history", &history); err != nil {
							return err
						}
						view, err := readExercise(ctx, exerciseURL, j.exercise)
						if err != nil {
							return err
						}
						if len(history) == 0 {
							return fmt.Errorf("exercise %s has no history", j.exercise)
						}
						if err := view.Rules.validate(); err != nil {
							return fmt.Errorf("read exercise %s: %w", j.exercise, err)
						}
						end, rules = view.Verdict, view.Rules
						rep.Note("read %d rounds", len(history))
						return nil
					},
				},
				{
					Intent: "Reconcile each assessment with the observer's record",
					Action: func(_ context.Context, rep *Reporter) error {
						lines, errs := checkTheater(j.exercise, history, end, g.read(), contactRounds, rules.Sight)
						for _, line := range lines {
							rep.Note("%s", line)
						}
						if errs > 0 {
							return fmt.Errorf("%d inconsistencies", errs)
						}
						return nil
					},
				},
			}, afterDrain(c, &l)
		},
	}
}

// assessmentLog collects one exercise's assessments as its reactor receives
// them, in the stream's order, and when the last of its events arrived.
type assessmentLog struct {
	exercise  string
	concluded *signal

	mu   sync.Mutex
	all  []assessment
	last time.Time
}

// handle keeps an assessment of the exercise and fires concluded on its
// conclusion, ignoring every other exercise's events.
func (g *assessmentLog) handle(_ context.Context, e event.Event) error {
	if exerciseOf(e) != g.exercise {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.last = time.Now()
	switch e.Type {
	case assessmentType:
		d, err := decode[assessment](e)
		if err != nil {
			return err
		}
		g.all = append(g.all, d)
	case concludedType:
		g.concluded.fire()
	}
	return nil
}

// since returns how long ago the exercise's last event arrived.
func (g *assessmentLog) since() time.Duration {
	g.mu.Lock()
	defer g.mu.Unlock()
	return time.Since(g.last)
}

// read returns the assessments collected so far.
func (g *assessmentLog) read() []assessment {
	g.mu.Lock()
	defer g.mu.Unlock()
	return slices.Clone(g.all)
}

// checkTheater renders the check of an exercise's assessments against its
// history: a header, the inconsistencies found, the observer's verdict and
// who truly holds each objective, and what each faction believes of each
// objective at the end against the truth. k is the rounds intelligence
// remembers a contact, and sight exercise's sight rule, by kind. It returns
// the lines and the number of inconsistencies.
func checkTheater(exercise string, history []observerRound, end *verdict, all []assessment, k int, sight map[string]int) ([]string, int) {
	history = slices.Clone(history)
	slices.SortFunc(history, func(a, b observerRound) int { return cmp.Compare(a.Round, b.Round) })
	latest := lastAssessments(all)
	out := []detail{text("theater  "+exercise).with(
		row(col("key", "rounds"), apart("value", strconv.Itoa(history[len(history)-1].Round+1))),
		row(col("key", "assessments"), apart("value", fmt.Sprintf("%d, %d revised", len(all), len(all)-len(latest)))),
		row(col("key", "contact rounds"), apart("value", strconv.Itoa(k))),
	)}
	errs := reconcile(history, latest, k, sight)
	if len(errs) == 0 {
		out = append(out, text("consistent  every assessment matches the observer's record under the suppression rules"))
	} else {
		found := text(fmt.Sprintf("inconsistencies  %d", len(errs)))
		for _, e := range errs {
			found = found.with(text(e))
		}
		out = append(out, found)
	}
	lines := list(out, "")
	lines = append(lines, truth(history[len(history)-1], end)...)
	lines = append(lines, beliefs(history, latest)...)
	return lines, len(errs)
}

// lastAssessments returns each faction's last assessment of each round,
// sorted by round, then faction: intelligence revises a round's assessment
// when a loss alert follows it, and the revision stands.
func lastAssessments(all []assessment) []assessment {
	last := map[factionRound]assessment{}
	for _, a := range all {
		last[factionRound{a.Faction, a.Round}] = a
	}
	out := slices.Collect(maps.Values(last))
	slices.SortFunc(out, func(a, b assessment) int {
		return cmp.Or(cmp.Compare(a.Round, b.Round), cmp.Compare(a.Faction, b.Faction))
	})
	return out
}

// reconcile returns every way the assessments, one per round and faction,
// depart from the history under the suppression rules:
//
//   - coverage: every round has an assessment of each faction;
//   - own: the faction's own elements are its true ones, with their true
//     health, strength, status, and cell;
//   - sees: every enemy element in the faction's observation of the round
//     is a contact of age 0, as the observer saw it, and no other contact is;
//   - remembers: an older contact is in no cell in sight, under the sight
//     rule, and no older than k rounds;
//   - objectives: every objective in sight is reported with its true
//     holder, none is reported before its faction first had it in sight,
//     and none out of sight is reported as seen this round. An objective a
//     loss alert told of is reported as the alert has it, in sight or not.
func reconcile(history []observerRound, assessments []assessment, k int, sight map[string]int) []string {
	factions := history[0].State.Factions
	rec := record{
		rounds:    map[int]observerRound{},
		factions:  factions,
		firstSeen: firstSightings(history, factions),
		alerts:    lossAlerts(history, factions),
	}
	for _, h := range history {
		rec.rounds[h.Round] = h
	}
	errs := checkCoverage(history, factions, assessments)
	for _, a := range assessments {
		errs = append(errs, rec.check(a, k, sight)...)
	}
	return errs
}

// record is what the check reads of exercise's history: each round, the
// factions, the first round each faction had each objective in sight, and
// the loss alerts each faction had of each objective.
type record struct {
	rounds    map[int]observerRound
	factions  []string
	firstSeen map[string]map[location]int
	alerts    map[string]map[location][]lossAlert
}

// lossAlert is exercise's alert to a faction that lost an objective: the
// round it lost it in, and its new holder.
type lossAlert struct {
	round  int
	holder string
}

// firstSightings returns the first round each faction had each objective
// in sight, by faction.
func firstSightings(history []observerRound, factions []string) map[string]map[location]int {
	out := map[string]map[location]int{}
	for _, f := range factions {
		out[f] = map[location]int{}
	}
	for _, h := range history {
		for i, o := range h.Observations {
			if i >= len(factions) {
				break
			}
			for _, obj := range o.Objectives {
				if _, ok := out[factions[i]][obj.At]; !ok {
					out[factions[i]][obj.At] = h.Round
				}
			}
		}
	}
	return out
}

// lossAlerts returns each objective's losses, by the faction that lost it:
// the alerts exercise sent it.
func lossAlerts(history []observerRound, factions []string) map[string]map[location][]lossAlert {
	out := map[string]map[location][]lossAlert{}
	for _, f := range factions {
		out[f] = map[location][]lossAlert{}
	}
	for _, h := range history {
		if h.Resolution == nil {
			continue
		}
		for _, c := range h.Resolution.Captures {
			if c.From != "" && out[c.From] != nil {
				out[c.From][c.At] = append(out[c.From][c.At], lossAlert{h.Round, c.Faction})
			}
		}
	}
	return out
}

// checkCoverage returns, for each round of the history, each faction no
// assessment of the round was issued for.
func checkCoverage(history []observerRound, factions []string, assessments []assessment) []string {
	issued := map[factionRound]bool{}
	for _, a := range assessments {
		issued[factionRound{a.Faction, a.Round}] = true
	}
	var errs []string
	for _, h := range history {
		for _, f := range factions {
			if !issued[factionRound{f, h.Round}] {
				errs = append(errs, fmt.Sprintf("round %d %s: no assessment was issued", h.Round, f))
			}
		}
	}
	return errs
}

// check returns every way one assessment departs from the faction's
// observation of its round: in its own elements, its contacts, and its
// objectives, each tagged with the round and the faction.
func (rec record) check(a assessment, k int, sight map[string]int) []string {
	tag := fmt.Sprintf("round %d %s", a.Round, a.Faction)
	h, ok := rec.rounds[a.Round]
	i := slices.Index(rec.factions, a.Faction)
	if !ok || i < 0 || i >= len(h.Observations) {
		return []string{tag + ": assessed, but exercise's history has no such round or faction"}
	}
	obs := h.Observations[i]
	var found []string
	if !sameElements(a.Own, obs.Own) {
		found = append(found, "own elements differ from the truth")
	}
	found = append(found, checkContacts(a, obs, k, sight)...)
	found = append(found, rec.checkObjectives(a, obs)...)
	for j, f := range found {
		found[j] = tag + ": " + f
	}
	return found
}

// checkContacts returns every way an assessment's contacts depart from what
// its faction saw: an enemy element in sight not reported so, a contact
// reported in sight that is not, and a remembered one in a cell in sight
// or remembered past k rounds.
func checkContacts(a assessment, obs observation, k int, sight map[string]int) []string {
	var found []string
	contacts := map[string]contact{}
	for _, c := range a.Contacts {
		contacts[c.ID] = c
	}
	visible := map[string]bool{}
	for _, e := range obs.Contacts {
		visible[e.ID] = true
		if c, ok := contacts[e.ID]; !ok || c.Age != 0 || !sameElement(c.element, e) {
			found = append(found, fmt.Sprintf("%s is in sight at %s, strength %d, but not reported so", e.ID, place(e.At), e.Strength))
		}
	}
	for _, c := range a.Contacts {
		if c.Age == 0 && !visible[c.ID] {
			found = append(found, fmt.Sprintf("%s is reported in sight, but is not", c.ID))
		}
		if c.Age > 0 && inSight(obs.Own, c.At, sight) {
			found = append(found, fmt.Sprintf("%s is remembered at %s, a cell in sight", c.ID, place(c.At)))
		}
		if c.Age > k {
			found = append(found, fmt.Sprintf("%s is remembered %d rounds, past %d", c.ID, c.Age, k))
		}
	}
	return found
}

// checkObjectives returns every way an assessment's objectives depart from
// what its faction saw and was told: an objective in sight not reported,
// or not with its true holder, one reported before the faction first had
// it in sight, and one out of sight reported as seen this round. An
// objective a loss alert told of stands as the alert has it.
func (rec record) checkObjectives(a assessment, obs observation) []string {
	r, f := a.Round, a.Faction
	alerted := func(o belief) bool {
		return slices.Contains(rec.alerts[f][o.At], lossAlert{o.Seen, o.Holder})
	}
	var found []string
	seen := map[location]string{}
	for _, o := range obs.Objectives {
		seen[o.At] = o.Holder
	}
	reported := map[location]bool{}
	for _, o := range a.Objectives {
		reported[o.At] = true
	}
	for _, o := range obs.Objectives {
		if !reported[o.At] {
			found = append(found, fmt.Sprintf("%s is in sight, but not reported", objectiveName(o.At)))
		}
	}
	for _, o := range a.Objectives {
		first, before := rec.firstSeen[f][o.At]
		before = before && first <= r
		holder, now := seen[o.At]
		switch {
		case alerted(o) && (!now || o.Seen > r):
			// A loss alert told of it; one of a later round than the
			// sighting stands over it.
		case now && (o.Age != 0 || o.Holder != holder):
			found = append(found, fmt.Sprintf("%s is in sight and %s, but not reported so", objectiveName(o.At), heldBy(holder)))
		case now:
		case !before:
			found = append(found, fmt.Sprintf("%s is reported, but %s has never had it in sight", objectiveName(o.At), f))
		case o.Age == 0:
			found = append(found, fmt.Sprintf("%s is reported seen this round, but is out of sight", objectiveName(o.At)))
		}
	}
	return found
}

// beliefs renders what each faction's last assessment believes of each
// objective, against who truly holds it after the last round: a block for
// each faction, as the theater narrates a round.
func beliefs(history []observerRound, assessments []assessment) []string {
	final := history[len(history)-1]
	last := map[string]assessment{}
	for _, a := range assessments {
		last[a.Faction] = a
	}
	var out []string
	for _, f := range final.State.Factions {
		known := map[location]belief{}
		for _, o := range last[f].Objectives {
			known[o.At] = o
		}
		var rows []detail
		for _, at := range final.State.objectives() {
			believed := "undiscovered"
			if o, ok := known[at]; ok {
				believed = orUnheld(o.Holder)
				if o.Age > 0 {
					believed += fmt.Sprintf(" (seen %d ago)", o.Age)
				}
			}
			rows = append(rows, row(col("objective", objectiveName(at)), apart("believed", believed),
				apart("truth", "truly "+orUnheld(final.State.Holders[place(at)]))))
		}
		out = append(out, f)
		out = append(out, labeled{label: "believes", items: list(rows, ""), each: true}.lines("  ", checkLabelWidth)...)
	}
	return out
}

// checkLabelWidth is the width to which the check pads the labels of its
// closing blocks: the longest of them, believes.
const checkLabelWidth = len("believes")

// truth renders the observer's block: the verdict, and who truly holds
// which objectives after the last round.
func truth(final observerRound, v *verdict) []string {
	held := map[string][]string{}
	for _, at := range final.State.objectives() {
		if h := final.State.Holders[place(at)]; h != "" {
			held[h] = append(held[h], objectiveName(at))
		}
	}
	ended := "none"
	if v != nil {
		winner := v.Winner
		if winner == "" {
			winner = "no winner"
		}
		ended = winner + " by " + v.Reason
	}
	holds := []string{"none"}
	if len(held) > 0 {
		holds = nil
	}
	for _, h := range slices.Sorted(maps.Keys(held)) {
		holds = append(holds, h+" "+strings.Join(held[h], " "))
	}
	out := []string{"observer"}
	out = append(out, labeled{label: "verdict", items: []string{ended}}.lines("  ", checkLabelWidth)...)
	return append(out, labeled{label: "holds", items: holds}.lines("  ", checkLabelWidth)...)
}

// sameElements reports whether a and b hold the same elements, in any
// order, as sameElement compares them.
func sameElements(a, b []element) bool {
	byID := func(x, y element) int { return cmp.Compare(x.ID, y.ID) }
	a, b = slices.Clone(a), slices.Clone(b)
	slices.SortFunc(a, byID)
	slices.SortFunc(b, byID)
	return slices.EqualFunc(a, b, sameElement)
}

// sameElement reports whether a and b agree in what the check compares of
// an element: its ID, strength, health, status, and cell.
func sameElement(a, b element) bool {
	return a.ID == b.ID && a.Strength == b.Strength && slices.Equal(a.Health, b.Health) && a.Status == b.Status && a.At == b.At
}

// inSight reports whether any of own sees at, under exercise's sight rule:
// the Chebyshev distance an element of each kind sees within its own
// sector.
func inSight(own []element, at location, sight map[string]int) bool {
	return slices.ContainsFunc(own, func(e element) bool {
		return e.At.Sector == at.Sector && max(abs(e.At.X-at.X), abs(e.At.Y-at.Y)) <= sight[e.Kind]
	})
}
