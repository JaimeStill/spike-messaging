package scenario

import (
	"context"
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

// AssessmentsDurable prefixes the durable consumer each assessments run
// subscribes under, so the stream's consumers show which are courier's.
const AssessmentsDurable = "courier-assessments-"

// assessmentsScenario joins the exercise services' stream and narrates the
// assessments the intelligence service issues for one exercise, a
// faction's or both, and the directives the command service decides on
// them, until the exercise concludes and the assessment of its last round
// has arrived.
func assessmentsScenario(joins Joins, needs func() []Need) Scenario {
	var exercise, faction, stream, prefix string
	maxAge := 24 * time.Hour
	wait := 2 * time.Minute
	return Scenario{
		Name:    "assessments",
		Summary: "Narrate the assessments issued for an exercise, and the directives decided on them, until it concludes",
		Needs:   needs,
		Flags: func(fs *pflag.FlagSet) {
			fs.StringVar(&exercise, "exercise", "", "the ID of the started exercise to follow (required)")
			fs.StringVar(&faction, "faction", "", "the faction whose assessments to narrate (default: both)")
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
			w := &assessmentWatch{
				exercise: exercise, faction: faction, standing: standing{},
				factions: onlyIf(faction),
				last:     map[string]int{}, prev: map[string]map[string]bool{}, prevOwn: map[string]map[string]bool{},
				first: newSignal(), concluded: newSignal(), done: newSignal(),
			}
			who := faction
			if who == "" {
				who = "either faction"
			}
			return []Step{
				{
					Intent: fmt.Sprintf("Join the %s stream, and wait for %s's first assessment of exercise %s", stream, who, exercise),
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
							Name:  AssessmentsDurable + strings.ReplaceAll(uuid.NewV7().String(), "-", ""),
							Types: []string{startedType, assessmentType, directiveType, concludedType},
						})
						if err != nil {
							return err
						}
						w.rep = rep
						corelifecycle.Register(c.lc, "watch", 0, reactor.New(src, w.handle, reactor.Grace(defaultGrace)))
						if err := c.start(ctx, rep); err != nil {
							return err
						}
						return c.await(ctx, w.first.ch, who+"'s first assessment")
					},
				},
				{
					Intent: "Narrate each assessment and directive as it arrives, until the exercise concludes and its last round is assessed",
					Action: func(ctx context.Context, _ *Reporter) error {
						wctx, cancel := context.WithTimeout(ctx, wait)
						defer cancel()
						select {
						case <-w.done.ch:
							return nil
						case <-wctx.Done():
							if w.concluded.isFired() {
								return fmt.Errorf("the assessments of round %d did not all arrive within %s", w.verdict().Round, wait)
							}
							return fmt.Errorf("exercise %s did not conclude within %s", exercise, wait)
						}
					},
				},
				{
					Intent: "Note the conclusion",
					Action: func(_ context.Context, rep *Reporter) error {
						v := w.verdict()
						if v.Winner == "" {
							rep.Note("concluded after round %d with no winner: %s", v.Round, v.Reason)
						} else {
							rep.Note("concluded after round %d: %s wins by %s", v.Round, v.Winner, v.Reason)
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

// assessmentWatch narrates one exercise's assessments as its reactor
// receives them and keeps what it needs to notice a faction's dropped
// contacts and to know when the last round has been assessed.
type assessmentWatch struct {
	exercise, faction      string
	first, concluded, done *signal
	rep                    *Reporter

	mu       sync.Mutex
	last     map[string]int             // the round of each faction's last narrated assessment
	prev     map[string]map[string]bool // the contacts of each faction's last narrated assessment
	prevOwn  map[string]map[string]bool // the own elements of each faction's last narrated assessment
	factions []string                   // the factions narrated: the start's, or --faction's
	standing standing                   // each faction's last directive, by element
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
		if r, ok := w.last[d.Faction]; ok && d.Round <= r {
			return nil
		}
		w.last[d.Faction] = d.Round
		w.narrate(d)
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
		if r, ok := w.last[f]; !ok || r < w.end.Round {
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
// faction's previous narrated assessment held that this one lacks, which
// the faction lost, and each contact it knew that this one lacks, which
// intelligence dropped. The caller holds mu.
func (w *assessmentWatch) narrate(d assessment) {
	w.rep.Note("%s", assessmentLine(d))
	own := ids(d.Own, func(e element) string { return e.ID })
	contacts := ids(d.Contacts, func(c contact) string { return c.ID })
	for _, id := range gone(w.prevOwn[d.Faction], own) {
		w.rep.Note("%s lost %s", d.Faction, id)
	}
	for _, id := range gone(w.prev[d.Faction], contacts) {
		w.rep.Note("%s dropped %s", d.Faction, id)
	}
	w.prevOwn[d.Faction] = own
	w.prev[d.Faction] = contacts
}

// gone returns, sorted, the IDs in before that now lacks.
func gone(before, now map[string]bool) []string {
	var out []string
	for id := range before {
		if !now[id] {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
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

func place(l location) string { return fmt.Sprintf("%s:%d,%d", l.Sector, l.X, l.Y) }

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
