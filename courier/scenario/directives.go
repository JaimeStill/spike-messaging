package scenario

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"
	"uuid"

	"github.com/spf13/pflag"

	"github.com/JaimeStill/spike-messaging/core/event"
	"github.com/JaimeStill/spike-messaging/messaging"
)

// DirectivesDurable prefixes the durable consumer each directives run
// subscribes under, so the stream's consumers show which are courier's.
const DirectivesDurable = "courier-directives-"

// commandSource is the source the directives scenario issues its directive
// from, standing in for the command service.
const commandSource = "/courier-command"

// directivesScenario stands in for the command service: it joins the
// exercise services' stream, reads a faction's first observation, issues a
// directive that sends each of the faction's elements to an objective the
// observation reports (or holds it, when the faction knows none), and waits
// for the exercise to conclude. The operations service turns the directive
// into orders, and the exercise service resolves them.
func directivesScenario(joins Joins, needs func() []Need) Scenario {
	j := newJoinFlags(2 * time.Minute)
	var faction string
	return Scenario{
		Name:    "directives",
		Summary: "Stand in for command: direct a faction's elements to the objectives it knows and watch the exercise conclude",
		Needs:   needs,
		Flags: func(fs *pflag.FlagSet) {
			j.bind(fs, "the ID of the started exercise to direct (required)", "how long to wait for the exercise to conclude")
			fs.StringVar(&faction, "faction", "", "the faction to direct (required)")
		},
		Validate: func() error {
			var factionErr error
			if faction == "" {
				factionErr = errors.New("--faction is required")
			}
			return j.validate(factionErr)
		},
		Steps: func() ([]Step, func() error) {
			c := newCoordinator(defaultDrain)
			var l lease
			var b messaging.Broker
			w := &watch{exercise: j.exercise, faction: faction, observed: newSignal(), concluded: newSignal()}
			return []Step{
				{
					Intent: fmt.Sprintf("Join the %s stream, and wait for %s's first observation of exercise %s", j.stream, faction, j.exercise),
					Action: func(ctx context.Context, rep *Reporter) error {
						var err error
						if b, err = l.join(ctx, rep, c, joins, j, DirectivesDurable, w.handle, observedType, concludedType); err != nil {
							return err
						}
						if err := c.await(ctx, w.observed.ch, faction+"'s first observation"); err != nil {
							return err
						}
						obs := w.first()
						rep.Note("round %d: %s has %d elements and reports %d objectives", obs.Round, faction, len(obs.Own), len(obs.Objectives))
						return nil
					},
				},
				{
					Intent: fmt.Sprintf("Direct each of %s's elements to the nearest known objective no other is heading for", faction),
					Action: func(ctx context.Context, rep *Reporter) error {
						d := w.directives()
						for _, dir := range d.Directives {
							if dir.Target == nil {
								rep.Note("%s holds", dir.Element)
								continue
							}
							rep.Note("%s heads for %s", dir.Element, place(*dir.Target))
						}
						data, err := json.Marshal(d)
						if err != nil {
							return err
						}
						return publish(ctx, b, rep, event.Event{
							ID: uuid.NewV7().String(), Source: commandSource, Type: directiveType,
							Subject: j.exercise, Time: time.Now(), DataContentType: "application/json", Data: data,
						})
					},
				},
				{
					Intent: "Wait for the exercise to conclude",
					Action: func(ctx context.Context, rep *Reporter) error {
						wctx, cancel := context.WithTimeout(ctx, j.wait)
						defer cancel()
						select {
						case <-w.concluded.ch:
						case <-wctx.Done():
							return fmt.Errorf("exercise %s did not conclude within %s", j.exercise, j.wait)
						}
						rep.Note("%s", conclusion(w.verdict()))
						return nil
					},
				},
				stopWatch(c),
			}, afterDrain(c, &l)
		},
	}
}

// watch collects what the directives scenario reads of one exercise: the
// faction's first observation, with the objectives it reports, and the
// verdict.
type watch struct {
	exercise, faction   string
	observed, concluded *signal

	mu  sync.Mutex
	obs *observed
	end concluded
}

// handle reads one event of the stream, ignoring every other exercise's.
func (w *watch) handle(_ context.Context, e event.Event) error {
	if exerciseOf(e) != w.exercise {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	switch e.Type {
	case observedType:
		d, err := decode[observed](e)
		if err != nil {
			return err
		}
		if d.Faction == w.faction && w.obs == nil {
			w.obs = &d
			w.observed.fire()
		}
	case concludedType:
		d, err := decode[concluded](e)
		if err != nil {
			return err
		}
		w.end = d
		w.concluded.fire()
	}
	return nil
}

func (w *watch) first() observed {
	w.mu.Lock()
	defer w.mu.Unlock()
	return *w.obs
}

func (w *watch) verdict() concluded {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.end
}

// directives assigns each element of the first observation, in ID order,
// the nearest objective the observation reports that no earlier element was
// assigned, or the nearest of all once every one is taken: an objective in
// the element's own sector by Manhattan distance first, then one in another
// sector. An element holds when the observation reports no objective.
func (w *watch) directives() directives {
	obs := w.first()
	var objectives []location
	for _, o := range obs.Objectives {
		objectives = append(objectives, o.At)
	}
	own := slices.Clone(obs.Own)
	slices.SortFunc(own, func(a, b element) int { return cmp.Compare(a.ID, b.ID) })
	taken := map[location]bool{}
	// The stand-in issues one directive per run, so its sequence is 1.
	d := directives{Exercise: w.exercise, Faction: w.faction, Round: obs.Round, Sequence: 1}
	for _, e := range own {
		free := slices.DeleteFunc(slices.Clone(objectives), func(o location) bool { return taken[o] })
		if len(free) == 0 {
			free = objectives
		}
		dir := directive{Element: e.ID}
		if len(free) > 0 {
			best := slices.MinFunc(free, func(a, b location) int { return cmp.Compare(distance(e.At, a), distance(e.At, b)) })
			taken[best] = true
			dir.Target = &best
		}
		d.Directives = append(d.Directives, dir)
	}
	return d
}

// distance orders objectives for an element at from: Manhattan distance
// within its own sector, and any other sector's beyond every one of its own.
func distance(from, to location) int {
	d := abs(to.X-from.X) + abs(to.Y-from.Y)
	if to.Sector != from.Sector {
		d += 1 << 20
	}
	return d
}
