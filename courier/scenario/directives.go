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
	"github.com/JaimeStill/spike-messaging/core/reactor"
	"github.com/JaimeStill/spike-messaging/messaging"
)

// Joins builds a broker on an existing stream, the one the exercise
// services share, with the release that frees the broker and anything the
// run left on the stream. Unlike a scratch broker's, the release leaves the
// stream and its events in place. The release may be nil.
type Joins func(stream, prefix string, maxAge time.Duration) (b messaging.Broker, release func() error, err error)

// DirectivesDurable prefixes the durable consumer each directives run
// subscribes under, so a release can find the ones a run left.
const DirectivesDurable = "courier-directives-"

// The events the directives scenario reads and the one it issues, and the
// source it issues it from, standing in for the command service.
const (
	startedType   = "exercise.started"
	observedType  = "exercise.round.observed"
	concludedType = "exercise.concluded"
	directiveType = "command.directive.issued"
	commandSource = "/courier-command"
)

// The scenario's own readings of exercise's payloads, as far as it reads
// them, and the directive it issues: the services share no Go types.
type (
	point struct {
		X int `json:"x"`
		Y int `json:"y"`
	}
	location struct {
		Sector string `json:"sector"`
		point
	}
	element struct {
		ID string   `json:"id"`
		At location `json:"at"`
	}
	startedData struct {
		Exercise string `json:"exercise"`
		Map      struct {
			Sectors []struct {
				ID         string  `json:"id"`
				Objectives []point `json:"objectives"`
			} `json:"sectors"`
		} `json:"map"`
	}
	observedData struct {
		Exercise string `json:"exercise"`
		Faction  string `json:"faction"`
		Round    int    `json:"round"`
		Own      []element `json:"own"`
	}
	concludedData struct {
		Exercise string `json:"exercise"`
		Round    int    `json:"round"`
		Winner   string `json:"winner"`
		Reason   string `json:"reason"`
	}
	directive struct {
		Element string    `json:"element"`
		Target  *location `json:"target"`
	}
	directiveData struct {
		Exercise   string      `json:"exercise"`
		Faction    string      `json:"faction"`
		Round      int         `json:"round"`
		Directives []directive `json:"directives"`
	}
)

// directivesScenario stands in for the command service: it joins the
// exercise services' stream, reads the map and a faction's first
// observation, issues a directive that sends each of the faction's
// elements to secure an objective, and waits for the exercise to conclude.
// operations turns the directive into orders, and exercise resolves them.
func directivesScenario(joins Joins, needs func() []Need) Scenario {
	var exercise, faction, stream, prefix string
	maxAge := 24 * time.Hour
	wait := 2 * time.Minute
	return Scenario{
		Name:    "directives",
		Summary: "Stand in for command: direct a faction's elements to the objectives and watch the exercise conclude",
		Needs:   needs,
		Flags: func(fs *pflag.FlagSet) {
			fs.StringVar(&exercise, "exercise", "", "the ID of the started exercise to direct (required)")
			fs.StringVar(&faction, "faction", "", "the faction to direct (required)")
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
			if faction == "" {
				errs = append(errs, errors.New("--faction is required"))
			}
			if wait <= 0 {
				errs = append(errs, errors.New("--wait must be positive"))
			}
			return errors.Join(errs...)
		},
		Steps: func() ([]Step, func() error) {
			c := newCoordinator(defaultDrain)
			var l lease
			var b messaging.Broker
			w := &watch{exercise: exercise, faction: faction, observed: newSignal(), concluded: newSignal()}
			return []Step{
				{
					Intent: fmt.Sprintf("Join the %s stream, and wait for %s's first observation of exercise %s", stream, faction, exercise),
					Action: func(ctx context.Context, rep *Reporter) error {
						if joins == nil {
							return errors.New("no stream to join is configured")
						}
						var err error
						if b, l.release, err = joins(stream, prefix, maxAge); err != nil {
							return err
						}
						sub := messaging.Subscription{
							Name:  DirectivesDurable + strings.ReplaceAll(uuid.NewV7().String(), "-", ""),
							Types: []string{startedType, observedType, concludedType},
						}
						src, err := b.Subscribe(sub)
						if err != nil {
							return err
						}
						c.add("watch", 0, reactor.New(src, w.handle, reactor.Grace(defaultGrace)))
						if err := c.start(ctx, rep); err != nil {
							return err
						}
						if err := c.await(ctx, w.observed.ch, faction+"'s first observation"); err != nil {
							return err
						}
						obs := w.first()
						rep.Note("round %d: %s has %d elements; the map has %d objectives", obs.Round, faction, len(obs.Own), len(w.objectives()))
						return nil
					},
				},
				{
					Intent: fmt.Sprintf("Direct each of %s's elements to the nearest objective no other is heading for", faction),
					Action: func(ctx context.Context, rep *Reporter) error {
						d := w.directives()
						for _, dir := range d.Directives {
							if dir.Target == nil {
								rep.Note("%s holds", dir.Element)
								continue
							}
							rep.Note("%s heads for %s:%d,%d", dir.Element, dir.Target.Sector, dir.Target.X, dir.Target.Y)
						}
						data, err := json.Marshal(d)
						if err != nil {
							return err
						}
						return publish(ctx, b, rep, event.Event{
							ID: uuid.NewV7().String(), Source: commandSource, Type: directiveType,
							Subject: exercise, Time: time.Now(), DataContentType: "application/json", Data: data,
						})
					},
				},
				{
					Intent: "Wait for the exercise to conclude",
					Action: func(ctx context.Context, rep *Reporter) error {
						wctx, cancel := context.WithTimeout(ctx, wait)
						defer cancel()
						select {
						case <-w.concluded.ch:
						case <-wctx.Done():
							return fmt.Errorf("exercise %s did not conclude within %s", exercise, wait)
						}
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

// watch collects what the directives scenario reads of one exercise: its
// map's objectives, the faction's first observation, and the verdict.
type watch struct {
	exercise, faction   string
	observed, concluded *signal

	mu      sync.Mutex
	started *startedData
	obs     *observedData
	end     concludedData
}

// handle reads one event of the stream, ignoring every other exercise's.
// The stream delivers the start before the observations it precedes, so
// the map is known by the first observation.
func (w *watch) handle(_ context.Context, e event.Event) error {
	var head struct {
		Exercise string `json:"exercise"`
	}
	if err := json.Unmarshal(e.Data, &head); err != nil || head.Exercise != w.exercise {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	switch e.Type {
	case startedType:
		var d startedData
		if err := json.Unmarshal(e.Data, &d); err != nil {
			return event.Permanent(err)
		}
		w.started = &d
	case observedType:
		var d observedData
		if err := json.Unmarshal(e.Data, &d); err != nil {
			return event.Permanent(err)
		}
		if d.Faction == w.faction && w.obs == nil && w.started != nil {
			w.obs = &d
			w.observed.fire()
		}
	case concludedType:
		if err := json.Unmarshal(e.Data, &w.end); err != nil {
			return event.Permanent(err)
		}
		w.concluded.fire()
	}
	return nil
}

func (w *watch) first() observedData {
	w.mu.Lock()
	defer w.mu.Unlock()
	return *w.obs
}

func (w *watch) verdict() concludedData {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.end
}

// objectives returns the map's objectives, sector by sector.
func (w *watch) objectives() []location {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []location
	for _, s := range w.started.Map.Sectors {
		for _, p := range s.Objectives {
			out = append(out, location{Sector: s.ID, point: p})
		}
	}
	return out
}

// directives assigns each element of the first observation, in ID order,
// the nearest objective no earlier element was assigned, or the nearest of
// all once every objective is taken: an objective in the element's own
// sector by Manhattan distance first, then one in another sector.
func (w *watch) directives() directiveData {
	obs := w.first()
	objectives := w.objectives()
	own := slices.Clone(obs.Own)
	slices.SortFunc(own, func(a, b element) int { return cmp.Compare(a.ID, b.ID) })
	taken := map[location]bool{}
	d := directiveData{Exercise: w.exercise, Faction: w.faction, Round: obs.Round}
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

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
