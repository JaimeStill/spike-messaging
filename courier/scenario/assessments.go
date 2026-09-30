package scenario

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/spf13/pflag"

	"github.com/JaimeStill/spike-messaging/core/event"
)

// AssessmentsDurable prefixes the durable consumer each assessments run
// subscribes under, so the stream's consumers show which are courier's.
const AssessmentsDurable = "courier-assessments-"

// assessmentsScenario joins the exercise services' stream and narrates the
// assessments the intelligence service issues for one exercise, a
// faction's or both, and the directives the command service decides on
// them, until the exercise concludes and the assessment of its last round
// has arrived.
func assessmentsScenario(joins Joins, needs func() []Need) Scenario {
	j := newJoinFlags(2 * time.Minute)
	var faction string
	return Scenario{
		Name:    "assessments",
		Summary: "Narrate the assessments issued for an exercise, and the directives decided on them, until it concludes",
		Needs:   needs,
		Flags: func(fs *pflag.FlagSet) {
			j.bind(fs, "the ID of the started exercise to follow (required)", "how long to wait for the exercise to conclude")
			fs.StringVar(&faction, "faction", "", "the faction whose assessments to narrate (default: both)")
		},
		Validate: func() error { return j.validate() },
		Steps: func() ([]Step, func() error) {
			c := newCoordinator(defaultDrain)
			var l lease
			w := &assessmentWatch{
				exercise: j.exercise, faction: faction, standing: standing{},
				factions: onlyIf(faction), last: map[string]assessment{},
				first: newSignal(), concluded: newSignal(), done: newSignal(),
			}
			who := faction
			if who == "" {
				who = "either faction"
			}
			return []Step{
				{
					Intent: fmt.Sprintf("Join the %s stream, and wait for %s's first assessment of exercise %s", j.stream, who, j.exercise),
					Action: func(ctx context.Context, rep *Reporter) error {
						w.rep = rep
						_, err := l.join(ctx, rep, c, joins, j, AssessmentsDurable, w.handle,
							startedType, assessmentType, directiveType, concludedType)
						if err != nil {
							return err
						}
						return c.await(ctx, w.first.ch, who+"'s first assessment")
					},
				},
				{
					Intent: "Narrate each assessment and directive as it arrives, until the exercise concludes and its last round is assessed",
					Action: func(ctx context.Context, _ *Reporter) error {
						wctx, cancel := context.WithTimeout(ctx, j.wait)
						defer cancel()
						select {
						case <-w.done.ch:
							return nil
						case <-wctx.Done():
							if w.concluded.isFired() {
								return fmt.Errorf("the assessments of round %d did not all arrive within %s", w.verdict().Round, j.wait)
							}
							return fmt.Errorf("exercise %s did not conclude within %s", j.exercise, j.wait)
						}
					},
				},
				{
					Intent: "Note the conclusion",
					Action: func(_ context.Context, rep *Reporter) error {
						rep.Note("%s", conclusion(w.verdict()))
						return nil
					},
				},
				stopWatch(c),
			}, afterDrain(c, &l)
		},
	}
}

// assessmentWatch narrates one exercise's assessments as its reactor
// receives them and keeps what it needs to notice a faction's dropped
// contacts and to know when the last round has been assessed.
type assessmentWatch struct {
	exercise, faction      string
	first, concluded, done *signal
	rep                    *Reporter

	mu       sync.Mutex
	last     map[string]assessment // each faction's last narrated assessment
	factions []string              // the factions narrated: the start's, or --faction's
	standing standing              // each faction's last directive, by element
	end      concluded
}

// handle reads one event of the stream, ignoring every other exercise's,
// and narrates an assessment of a round later than its faction's last
// narrated one. It is called from the reactor's goroutine; the Reporter is
// safe for that.
func (w *assessmentWatch) handle(_ context.Context, e event.Event) error {
	if exerciseOf(e) != w.exercise {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	switch e.Type {
	case startedType:
		d, err := decode[started](e)
		if err != nil {
			return err
		}
		if len(w.factions) == 0 {
			w.factions = d.Factions
		}
	case assessmentType:
		d, err := decode[assessment](e)
		if err != nil {
			return err
		}
		if w.faction != "" && d.Faction != w.faction {
			return nil
		}
		if a, ok := w.last[d.Faction]; ok && d.Round <= a.Round {
			return nil
		}
		w.narrate(d)
		w.last[d.Faction] = d
		w.first.fire()
	case directiveType:
		d, err := decode[directives](e)
		if err != nil {
			return err
		}
		if w.faction != "" && d.Faction != w.faction {
			return nil
		}
		var lines []string
		for _, x := range w.standing.changes(d) {
			verb, target, contact := act(x, nil, place)
			lines = append(lines, strings.Join(slices.DeleteFunc([]string{x.Element, verb, target, contact}, func(s string) bool { return s == "" }), " "))
		}
		if len(lines) > 0 {
			w.rep.Note("round %d %s directs: %s", d.Round, d.Faction, strings.Join(lines, " · "))
		}
	case concludedType:
		d, err := decode[concluded](e)
		if err != nil {
			return err
		}
		w.end = d
		if len(w.factions) == 0 {
			// The start aged out of the stream: wait for the factions whose
			// assessments were narrated.
			for f := range w.last {
				w.factions = append(w.factions, f)
			}
			slices.Sort(w.factions)
		}
		w.concluded.fire()
	}
	w.check()
	return nil
}

// check fires done once the exercise has concluded and each faction's last
// narrated assessment is of the concluded round or later. The factions are
// those the start named, or the one --faction names.
func (w *assessmentWatch) check() {
	if !w.concluded.isFired() {
		return
	}
	for _, f := range w.factions {
		if a, ok := w.last[f]; !ok || a.Round < w.end.Round {
			return
		}
	}
	w.done.fire()
}

func (w *assessmentWatch) verdict() concluded {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.end
}

// narrate notes the assessment in full, then each own element the
// faction's last narrated assessment held that this one lacks, which the
// faction lost, and each contact it knew that this one lacks, which
// intelligence dropped, each by ID. The caller holds mu, and records d as
// the faction's last narrated assessment after.
func (w *assessmentWatch) narrate(d assessment) {
	w.rep.Note("%s", assessmentLine(d))
	prev := w.last[d.Faction]
	lost := gone(prev.Own, ids(d.Own, elementID), elementID)
	dropped := gone(prev.Contacts, ids(d.Contacts, contactID), contactID)
	slices.Sort(lost)
	slices.Sort(dropped)
	for _, id := range lost {
		w.rep.Note("%s lost %s", d.Faction, id)
	}
	for _, id := range dropped {
		w.rep.Note("%s dropped %s", d.Faction, id)
	}
}

// assessmentLine renders an assessment on one line: the faction's own
// elements, the contacts it knows, and the objectives.
func assessmentLine(d assessment) string {
	var own, contacts, objectives []string
	for _, e := range d.Own {
		own = append(own, fmt.Sprintf("%s %s", e.ID, place(e.At)))
	}
	for _, c := range d.Contacts {
		contacts = append(contacts, fmt.Sprintf("%s %s seen %d age %d", c.ID, place(c.At), c.Seen, c.Age))
	}
	for _, o := range d.Objectives {
		s := place(o.At)
		if o.Holder == "" {
			s += " unheld"
		} else {
			s += " " + o.Holder
		}
		if o.Age > 0 {
			s += fmt.Sprintf(" age %d", o.Age)
		}
		objectives = append(objectives, s)
	}
	return fmt.Sprintf("round %d %s: own %s | contacts %s | objectives %s",
		d.Round, d.Faction, orNone(own, " · "), orNone(contacts, " · "), orNone(objectives, " · "))
}

func orNone(parts []string, sep string) string {
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, sep)
}

// onlyIf returns a list of f alone, or none when f is empty.
func onlyIf(f string) []string {
	if f == "" {
		return nil
	}
	return []string{f}
}
