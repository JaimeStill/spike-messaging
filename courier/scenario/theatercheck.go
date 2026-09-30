package scenario

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
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

// TheaterCheckDurable prefixes the durable consumer each theater-check run
// subscribes under. Each run's durable is new, so it reads the stream from
// its beginning.
const TheaterCheckDurable = "courier-theater-check-"

// sight is the Chebyshev distance an element of each kind sees within its
// own sector. It mirrors exercise's rule (rules.Kind.Sight); the check
// needs it only to find a remembered contact in a cell in sight, since the
// umpire's observations already list what each faction sees.
var sight = map[string]int{"squad": 1, "scout": 2}

// The theater check's own readings of exercise's history and view, and of
// intelligence's assessment in full: the services share no Go types.
type (
	umpireElement struct {
		ID       string   `json:"id"`
		Kind     string   `json:"kind"`
		Strength int      `json:"strength"`
		Health   []int    `json:"health"`
		Status   string   `json:"status"`
		At       location `json:"at"`
	}
	umpireObjective struct {
		At     location `json:"at"`
		Holder string   `json:"holder"`
	}
	umpireObservation struct {
		Own        []umpireElement   `json:"own"`
		Contacts   []umpireElement   `json:"contacts"`
		Objectives []umpireObjective `json:"objectives"`
	}
	// umpireRound is one round of exercise's history: the state after it,
	// each faction's observation of it, indexed like the factions, and its
	// resolution, nil for round 0.
	umpireRound struct {
		Round int `json:"round"`
		State struct {
			Map struct {
				Sectors []struct {
					ID         string  `json:"id"`
					Objectives []point `json:"objectives"`
				} `json:"sectors"`
			} `json:"map"`
			Factions []string          `json:"factions"`
			Holders  map[string]string `json:"holders"`
		} `json:"state"`
		Observations []umpireObservation `json:"observations"`
		Resolution   *struct {
			Captures []struct {
				At      location `json:"at"`
				Faction string   `json:"faction"`
				From    string   `json:"from"`
			} `json:"captures"`
		} `json:"resolution"`
	}
	umpireVerdict struct {
		Winner string `json:"winner"`
		Reason string `json:"reason"`
	}
	checkedContact struct {
		umpireElement
		Seen int `json:"seen"`
		Age  int `json:"age"`
	}
	checkedAssessment struct {
		Exercise   string              `json:"exercise"`
		Faction    string              `json:"faction"`
		Round      int                 `json:"round"`
		Own        []umpireElement     `json:"own"`
		Contacts   []checkedContact    `json:"contacts"`
		Objectives []assessedObjective `json:"objectives"`
	}
)

// theaterCheckScenario reconciles a theater run's assessments against
// exercise's history, the umpire's record. It reads the assessments from
// the stream, from its beginning, and the history from exercise's API, and
// fails on any assessment the suppression rules do not explain.
func theaterCheckScenario(joins Joins, needs func() []Need) Scenario {
	var exercise, exerciseURL, stream, prefix string
	contactRounds := 3
	maxAge := 24 * time.Hour
	wait := time.Minute
	idle := 2 * time.Second
	return Scenario{
		Name:    "theater-check",
		Summary: "Reconcile an exercise's assessments against the umpire's record, and state what each faction believes at the end",
		Needs:   needs,
		Flags: func(fs *pflag.FlagSet) {
			fs.StringVar(&exercise, "exercise", "", "the ID of the exercise to check (required)")
			fs.StringVar(&exerciseURL, "exercise-url", "http://localhost:8081", "the exercise service's base URL")
			fs.IntVar(&contactRounds, "contact-rounds", contactRounds, "the rounds intelligence remembers a contact, as it is configured")
			fs.StringVar(&stream, "stream", "exercise", "the stream the exercise services share")
			fs.StringVar(&prefix, "prefix", "exercise", "the subject prefix of the services' stream")
			fs.DurationVar(&maxAge, "max-age", maxAge, "the stream's max_age, as the services configure it: the last broker to provision the stream sets it")
			fs.DurationVar(&wait, "wait", wait, "how long to wait for the exercise's conclusion and the assessments after it")
			fs.DurationVar(&idle, "idle", idle, "how long the stream must be quiet after the conclusion before the assessments are complete")
		},
		Validate: func() error {
			var errs []error
			if _, err := uuid.Parse(exercise); err != nil {
				errs = append(errs, errors.New("--exercise must be an exercise's ID"))
			}
			if u, err := url.Parse(exerciseURL); err != nil || u.Scheme == "" || u.Host == "" {
				errs = append(errs, errors.New("--exercise-url must be an absolute URL"))
			}
			if contactRounds < 0 {
				errs = append(errs, errors.New("--contact-rounds must not be negative"))
			}
			if wait <= 0 || idle <= 0 {
				errs = append(errs, errors.New("--wait and --idle must be positive"))
			}
			return errors.Join(errs...)
		},
		Steps: func() ([]Step, func() error) {
			c := newCoordinator(defaultDrain)
			var l lease
			g := &assessmentLog{exercise: exercise, concluded: newSignal()}
			var history []umpireRound
			var verdict *umpireVerdict
			return []Step{
				{
					Intent: fmt.Sprintf("Join the %s stream from its beginning, and read exercise %s's assessments until it concludes and the stream is quiet", stream, exercise),
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
							Name:  TheaterCheckDurable + strings.ReplaceAll(uuid.NewV7().String(), "-", ""),
							Types: []string{assessmentType, concludedType},
						})
						if err != nil {
							return err
						}
						corelifecycle.Register(c.lc, "watch", 0, reactor.New(src, g.handle, reactor.Grace(defaultGrace)))
						if err := c.start(ctx, rep); err != nil {
							return err
						}
						wctx, cancel := context.WithTimeout(ctx, wait)
						defer cancel()
						select {
						case <-g.concluded.ch:
						case <-wctx.Done():
							return fmt.Errorf("exercise %s did not conclude within %s", exercise, wait)
						}
						// The final round's assessments, and any revision, follow
						// the conclusion.
						for quiet := idle - g.since(); quiet > 0; quiet = idle - g.since() {
							if !sleep(wctx, quiet) {
								rep.Note("the stream was not quiet for %s within %s", idle, wait)
								break
							}
						}
						rep.Note("read %d assessments", len(g.read()))
						return nil
					},
				},
				{
					Intent: "Signal the drain: the watch stops receiving",
					Action: func(_ context.Context, rep *Reporter) error { return c.stop(rep) },
				},
				{
					Intent: fmt.Sprintf("Read the umpire's record of exercise %s from %s", exercise, exerciseURL),
					Action: func(ctx context.Context, rep *Reporter) error {
						base := strings.TrimSuffix(exerciseURL, "/") + "/api/exercises/" + exercise
						if err := getJSON(ctx, base+"/history", &history); err != nil {
							return err
						}
						var view struct {
							Verdict *umpireVerdict `json:"verdict"`
						}
						if err := getJSON(ctx, base, &view); err != nil {
							return err
						}
						if len(history) == 0 {
							return fmt.Errorf("exercise %s has no history", exercise)
						}
						verdict = view.Verdict
						rep.Note("read %d rounds", len(history))
						return nil
					},
				},
				{
					Intent: "Reconcile each assessment with the umpire's record",
					Action: func(_ context.Context, rep *Reporter) error {
						lines, errs := checkTheater(exercise, history, verdict, g.read(), contactRounds)
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
	all  []checkedAssessment
	last time.Time
}

// handle keeps an assessment of the exercise and fires concluded on its
// conclusion, ignoring every other exercise's events.
func (g *assessmentLog) handle(_ context.Context, e event.Event) error {
	var head struct {
		Exercise string `json:"exercise"`
	}
	if err := json.Unmarshal(e.Data, &head); err != nil || head.Exercise != g.exercise {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.last = time.Now()
	switch e.Type {
	case assessmentType:
		var d checkedAssessment
		if err := json.Unmarshal(e.Data, &d); err != nil {
			return event.Permanent(err)
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
func (g *assessmentLog) read() []checkedAssessment {
	g.mu.Lock()
	defer g.mu.Unlock()
	return slices.Clone(g.all)
}

// getJSON decodes the JSON body of a GET of u into v.
func getJSON(ctx context.Context, u string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", u, res.Status)
	}
	if err := json.NewDecoder(res.Body).Decode(v); err != nil {
		return fmt.Errorf("GET %s: %w", u, err)
	}
	return nil
}

// checkTheater renders the check of an exercise's assessments against its
// history: a header, the inconsistencies found, what each faction believes
// of each objective at the end against the truth, and the verdict. It
// returns the lines and the number of inconsistencies.
func checkTheater(exercise string, history []umpireRound, verdict *umpireVerdict, all []checkedAssessment, k int) ([]string, int) {
	history = slices.Clone(history)
	slices.SortFunc(history, func(a, b umpireRound) int { return cmp.Compare(a.Round, b.Round) })
	latest := lastAssessments(all)
	lines := []string{fmt.Sprintf("theater %s: %d rounds, %d assessments (%d revised), contact_rounds %d",
		exercise, history[len(history)-1].Round+1, len(all), len(all)-len(latest), k)}
	errs := reconcile(history, latest, k)
	if len(errs) == 0 {
		lines = append(lines, "consistent: every assessment matches the umpire's record under the suppression rules")
	} else {
		lines = append(lines, fmt.Sprintf("%d inconsistencies:", len(errs)))
		for _, e := range errs {
			lines = append(lines, "  "+e)
		}
	}
	lines = append(lines, "", "what each faction believes at the end, against the truth:")
	lines = append(lines, beliefs(history, latest)...)
	lines = append(lines, "", verdictLine(history[len(history)-1], verdict))
	return lines, len(errs)
}

// lastAssessments returns each faction's last assessment of each round,
// sorted by round, then faction: intelligence revises a round's assessment
// when a loss alert follows it, and the revision stands.
func lastAssessments(all []checkedAssessment) []checkedAssessment {
	type key struct {
		round   int
		faction string
	}
	last := map[key]checkedAssessment{}
	for _, a := range all {
		last[key{a.Round, a.Faction}] = a
	}
	out := slices.Collect(maps.Values(last))
	slices.SortFunc(out, func(a, b checkedAssessment) int {
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
//     is a contact of age 0, as the umpire saw it, and no other contact is;
//   - remembers: an older contact is in no cell in sight, and no older than
//     k rounds;
//   - objectives: every objective in sight is reported with its true
//     holder, none is reported before its faction first had it in sight,
//     and none out of sight is reported as seen this round. An objective a
//     loss alert told of is reported as the alert has it, in sight or not.
func reconcile(history []umpireRound, assessments []checkedAssessment, k int) []string {
	rounds := map[int]umpireRound{}
	for _, h := range history {
		rounds[h.Round] = h
	}
	factions := history[0].State.Factions
	// firstSeen is the first round each faction had each objective in
	// sight, and alerts each objective's losses, by faction.
	firstSeen := map[string]map[string]int{}
	type loss struct {
		round  int
		holder string
	}
	alerts := map[string]map[string][]loss{}
	for _, f := range factions {
		firstSeen[f] = map[string]int{}
		alerts[f] = map[string][]loss{}
	}
	for _, h := range history {
		for i, o := range h.Observations {
			if i >= len(factions) {
				break
			}
			for _, obj := range o.Objectives {
				if _, ok := firstSeen[factions[i]][place(obj.At)]; !ok {
					firstSeen[factions[i]][place(obj.At)] = h.Round
				}
			}
		}
		if h.Resolution == nil {
			continue
		}
		for _, c := range h.Resolution.Captures {
			if c.From != "" && alerts[c.From] != nil {
				alerts[c.From][place(c.At)] = append(alerts[c.From][place(c.At)], loss{h.Round, c.Faction})
			}
		}
	}
	alerted := func(f string, o assessedObjective) bool {
		return slices.Contains(alerts[f][place(o.At)], loss{o.Seen, o.Holder})
	}

	var errs []string
	issued := map[string]bool{}
	for _, a := range assessments {
		issued[fmt.Sprint(a.Round, a.Faction)] = true
	}
	for _, h := range history {
		for _, f := range factions {
			if !issued[fmt.Sprint(h.Round, f)] {
				errs = append(errs, fmt.Sprintf("round %d %s: no assessment was issued", h.Round, f))
			}
		}
	}
	for _, a := range assessments {
		r, f := a.Round, a.Faction
		tag := fmt.Sprintf("round %d %s", r, f)
		h, ok := rounds[r]
		i := slices.Index(factions, f)
		if !ok || i < 0 || i >= len(h.Observations) {
			errs = append(errs, tag+": assessed, but exercise's history has no such round or faction")
			continue
		}
		obs := h.Observations[i]
		fail := func(format string, args ...any) { errs = append(errs, tag+": "+fmt.Sprintf(format, args...)) }

		if !slices.Equal(elementKeys(a.Own), elementKeys(obs.Own)) {
			fail("own elements differ from the truth")
		}
		contacts := map[string]checkedContact{}
		for _, c := range a.Contacts {
			contacts[c.ID] = c
		}
		visible := map[string]bool{}
		for _, e := range obs.Contacts {
			visible[e.ID] = true
			if c, ok := contacts[e.ID]; !ok || c.Age != 0 || elementKey(c.umpireElement) != elementKey(e) {
				fail("%s is in sight at %s, strength %d, but not reported so", e.ID, place(e.At), e.Strength)
			}
		}
		for _, c := range a.Contacts {
			if c.Age == 0 && !visible[c.ID] {
				fail("%s is reported in sight, but is not", c.ID)
			}
			if c.Age > 0 && inSight(obs.Own, c.At) {
				fail("%s is remembered at %s, a cell in sight", c.ID, place(c.At))
			}
			if c.Age > k {
				fail("%s is remembered %d rounds, past %d", c.ID, c.Age, k)
			}
		}

		seen := map[string]string{}
		for _, o := range obs.Objectives {
			seen[place(o.At)] = o.Holder
		}
		reported := map[string]bool{}
		for _, o := range a.Objectives {
			reported[place(o.At)] = true
		}
		for _, o := range obs.Objectives {
			if !reported[place(o.At)] {
				fail("%s is in sight, but not reported", objectiveName(o.At))
			}
		}
		for _, o := range a.Objectives {
			p := place(o.At)
			first, before := firstSeen[f][p]
			before = before && first <= r
			holder, now := seen[p]
			switch {
			case alerted(f, o) && (!now || o.Seen > r):
				// A loss alert told of it; one of a later round than the
				// sighting stands over it.
			case now && (!o.Known || o.Age != 0 || o.Holder != holder):
				fail("%s is in sight and %s, but not reported so", objectiveName(o.At), heldBy(holder))
			case now:
			case !before:
				fail("%s is reported, but %s has never had it in sight", objectiveName(o.At), f)
			case o.Known && o.Age == 0:
				fail("%s is reported seen this round, but is out of sight", objectiveName(o.At))
			case !o.Known:
				fail("%s is unknown, but was seen before", objectiveName(o.At))
			}
		}
	}
	return errs
}

// beliefs renders what each faction's last assessment believes of each
// objective, against who truly holds it after the last round.
func beliefs(history []umpireRound, assessments []checkedAssessment) []string {
	final := history[len(history)-1]
	last := map[string]checkedAssessment{}
	for _, a := range assessments {
		last[a.Faction] = a
	}
	width := 0
	for _, f := range final.State.Factions {
		width = max(width, len(f))
	}
	var lines []string
	for _, f := range final.State.Factions {
		known := map[string]assessedObjective{}
		for _, o := range last[f].Objectives {
			known[place(o.At)] = o
		}
		for _, sec := range final.State.Map.Sectors {
			for _, p := range sec.Objectives {
				at := location{Sector: sec.ID, point: p}
				believed := "undiscovered"
				o, ok := known[place(at)]
				switch {
				case ok && o.Known:
					believed = orUnheld(o.Holder)
					if o.Age > 0 {
						believed += fmt.Sprintf(", seen %d rounds ago", o.Age)
					}
				case ok:
					believed = "unknown"
				}
				lines = append(lines, fmt.Sprintf("  %-*s  %-14s believes %s; truly %s",
					width, f, objectiveName(at), believed, orUnheld(final.State.Holders[place(at)])))
			}
		}
	}
	return lines
}

// verdictLine renders the verdict and who truly holds which objective after
// the last round.
func verdictLine(final umpireRound, v *umpireVerdict) string {
	held := map[string][]string{}
	for _, sec := range final.State.Map.Sectors {
		for _, p := range sec.Objectives {
			at := location{Sector: sec.ID, point: p}
			if h := final.State.Holders[place(at)]; h != "" {
				held[h] = append(held[h], objectiveName(at))
			}
		}
	}
	var holds []string
	for _, h := range slices.Sorted(maps.Keys(held)) {
		holds = append(holds, fmt.Sprintf("%s %d (%s)", h, len(held[h]), strings.Join(held[h], " ")))
	}
	head := "verdict: none"
	if v != nil {
		winner := v.Winner
		if winner == "" {
			winner = "no winner"
		}
		head = fmt.Sprintf("verdict: %s by %s", winner, v.Reason)
	}
	return head + "; truly held: " + orNone(holds, "; ")
}

// elementKeys returns each element's key, sorted.
func elementKeys(es []umpireElement) []string {
	keys := make([]string, len(es))
	for i, e := range es {
		keys[i] = elementKey(e)
	}
	slices.Sort(keys)
	return keys
}

// elementKey renders what the check compares of an element: its ID,
// strength, health, status, and cell.
func elementKey(e umpireElement) string {
	return fmt.Sprintf("%s %d %v %s %s", e.ID, e.Strength, e.Health, e.Status, place(e.At))
}

// inSight reports whether any of own sees at, under exercise's sight rule.
func inSight(own []umpireElement, at location) bool {
	return slices.ContainsFunc(own, func(e umpireElement) bool {
		return e.At.Sector == at.Sector && max(abs(e.At.X-at.X), abs(e.At.Y-at.Y)) <= sight[e.Kind]
	})
}

// objectiveName names an objective's cell as the narration does.
func objectiveName(l location) string { return fmt.Sprintf("objective:%d,%d", l.X, l.Y) }

func heldBy(holder string) string {
	if holder == "" {
		return "unheld"
	}
	return "held by " + holder
}

func orUnheld(holder string) string {
	if holder == "" {
		return "unheld"
	}
	return holder
}
