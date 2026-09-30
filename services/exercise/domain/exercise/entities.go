package exercise

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/standards-lab/sqlate"

	"github.com/JaimeStill/spike-messaging/services/exercise/domain/exercise/rules"
)

// ErrValidation classifies a command input that the domain rejects because
// of its own fields. The returned error wraps it with every reason found.
var ErrValidation = errors.New("invalid command")

// ErrNotFound reports an exercise that does not exist. It wraps
// sql.ErrNoRows, so a caller that tests for either finds it.
var ErrNotFound = fmt.Errorf("exercise not found: %w", sql.ErrNoRows)

// ErrConflict reports a command the exercise's current status does not
// allow, such as a start of an exercise already running, or an order for a
// round already resolved.
var ErrConflict = errors.New("the command conflicts with the exercise's status")

// MinRoundInterval is the shortest round interval an exercise accepts.
const MinRoundInterval = 10 * time.Millisecond

// Status is where an exercise is in its life: created, then running and
// paused in turn, and finally concluded by a verdict or stopped.
type Status string

// The statuses of an exercise.
const (
	// StatusCreated is an exercise that has not started: no round is observed.
	StatusCreated Status = "created"
	// StatusRunning is an exercise whose rounds resolve at its interval.
	StatusRunning Status = "running"
	// StatusPaused is a started exercise whose rounds are held until it resumes.
	StatusPaused Status = "paused"
	// StatusConcluded is an exercise a verdict ended.
	StatusConcluded Status = "concluded"
	// StatusStopped is an exercise the umpire ended before its verdict.
	StatusStopped Status = "stopped"
)

// reasonStopped is the verdict's reason for a stopped exercise.
const reasonStopped = "stopped"

// CreateExercise is the create command's input: the exercise's name, its
// seed, its public map, its two factions, their elements where they stand
// at the start, its round interval as a Go duration such as "2s", and its
// round limit.
//
// Seed is optional: Create draws one when it is absent. Map and Elements
// are optional together: when both are absent, the exercise starts from the
// skirmish that [rules.Skirmish] lays out from the seed, and when both are
// given, it starts from them as they are.
type CreateExercise struct {
	Name          string          `json:"name"`
	Seed          *int64          `json:"seed,omitempty"`
	Map           *rules.Map      `json:"map,omitempty"`
	Factions      [2]string       `json:"factions"`
	Elements      []rules.Element `json:"elements,omitempty"`
	RoundInterval string          `json:"round_interval"`
	RoundLimit    int             `json:"round_limit"`
}

// Validate reports every way c breaks the command's rules, wrapped in
// [ErrValidation]: the name is non-empty; the interval parses as a Go
// duration of at least [MinRoundInterval]; the limit is at least 1; the map
// and the elements are both given or both absent; and the starting state is
// valid under [rules.State.Validate]. The skirmish's layout is valid for
// any two distinct, named factions, so for it only the factions are
// checked.
func (c CreateExercise) Validate() error {
	var errs []error
	if strings.TrimSpace(c.Name) == "" {
		errs = append(errs, errors.New("name: empty"))
	}
	if _, err := c.interval(); err != nil {
		errs = append(errs, err)
	}
	if c.RoundLimit < 1 {
		errs = append(errs, fmt.Errorf("round_limit: %d, want at least 1", c.RoundLimit))
	}
	if (c.Map == nil) != (c.Elements == nil) {
		errs = append(errs, errors.New("map and elements: give both, or neither for the skirmish"))
	} else {
		s := c.state(0)
		if c.Map == nil {
			s.Elements = nil
		}
		if err := s.Validate(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%w: %w", ErrValidation, errors.Join(errs...))
	}
	return nil
}

// interval parses the command's round interval.
func (c CreateExercise) interval() (time.Duration, error) {
	d, err := time.ParseDuration(c.RoundInterval)
	if err != nil {
		return 0, fmt.Errorf("round_interval: %w", err)
	}
	if d < MinRoundInterval {
		return 0, fmt.Errorf("round_interval: %s, want at least %s", d, MinRoundInterval)
	}
	return d, nil
}

// state is the exercise's starting state for seed: the skirmish the seed
// lays out when the command gives no map, or else the command's map and
// elements. No objective is held.
func (c CreateExercise) state(seed int64) rules.State {
	if c.Map == nil {
		return rules.Skirmish(seed, c.Factions)
	}
	return rules.State{
		Map:      *c.Map,
		Factions: c.Factions,
		Elements: c.Elements,
		Holders:  map[string]string{},
		Progress: map[string]rules.Progress{},
	}
}

// Exercise is the umpire's full view of an exercise: its settings, its
// status, the last round resolved, the world as that round left it, the
// verdict once it is over, and when its next round is due while it runs.
// Seed is the seed every round's random draws come from, so it replays the
// exercise. RoundInterval is a Go duration, such as "2s". Rules holds the
// constants of the rules that a client reads the view by.
type Exercise struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Seed          int64          `json:"seed"`
	Status        Status         `json:"status"`
	Round         int            `json:"round"`
	RoundLimit    int            `json:"round_limit"`
	RoundInterval string         `json:"round_interval"`
	Factions      [2]string      `json:"factions"`
	State         rules.State    `json:"state"`
	Rules         Rules          `json:"rules"`
	Verdict       *rules.Verdict `json:"verdict"`
	NextRoundAt   *time.Time     `json:"next_round_at"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`

	// intervalMS is RoundInterval in milliseconds, as the database keeps it.
	intervalMS int64
}

// Rules holds the constants of the rules that a client needs to read an
// exercise's state and history, so that it never mirrors them by hand.
// CaptureRounds is [rules.CaptureRounds], the rounds in a row a faction must
// end alone on an objective to take it. Sight is each kind's
// [rules.Kind.Sight], the distance an element of the kind sees.
type Rules struct {
	CaptureRounds int                `json:"capture_rounds"`
	Sight         map[rules.Kind]int `json:"sight"`
}

// currentRules returns the rules the package resolves every exercise by.
func currentRules() Rules {
	r := Rules{CaptureRounds: rules.CaptureRounds, Sight: make(map[rules.Kind]int, len(rules.Kinds))}
	for _, k := range rules.Kinds {
		r.Sight[k] = k.Sight()
	}
	return r
}

// started reports whether the exercise is running or paused: it has
// started and has not yet ended.
func (e Exercise) started() bool {
	return e.Status == StatusRunning || e.Status == StatusPaused
}

// Round is one entry of an exercise's history: the state after the round,
// each faction's observation of it, indexed like the exercise's factions,
// the verdict, and the round's resolution. Round 0 is the start, which
// resolves nothing, so its Resolution is nil.
type Round struct {
	Round        int                  `json:"round"`
	State        rules.State          `json:"state"`
	Observations [2]rules.Observation `json:"observations"`
	Verdict      rules.Verdict        `json:"verdict"`
	Resolution   *rules.Resolution    `json:"resolution"`
	ResolvedAt   time.Time            `json:"resolved_at"`
}

// RecordOrders is the command that records one faction's orders for one
// round of an exercise. The consuming reactor decodes it from the data of
// the orders event an operations service issued; the domain never sees the
// event.
type RecordOrders struct {
	Exercise string        `json:"exercise"`
	Faction  string        `json:"faction"`
	Round    int           `json:"round"`
	Orders   []rules.Order `json:"orders"`
}

// Claim is an idempotency claim that a command runs in its transaction
// before it does anything else. It reports whether this is the first time
// the command's input was handled. On false the command changes nothing and
// succeeds. The consuming reactor binds a Claim over its inbox and the
// event it handles. A caller without an inbox, such as a test, passes a nil
// Claim, which claims nothing. It is an alias, so the claim that
// messaging's Consume supplies to a consumer is a Claim.
type Claim = func(ctx context.Context, tx *sqlate.Tx) (first bool, err error)
