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
// public map, its two factions, their elements where they stand at the
// start, its round interval as a Go duration such as "2s", and its round
// limit.
type CreateExercise struct {
	Name          string          `json:"name"`
	Map           rules.Map       `json:"map"`
	Factions      [2]string       `json:"factions"`
	Elements      []rules.Element `json:"elements"`
	RoundInterval string          `json:"round_interval"`
	RoundLimit    int             `json:"round_limit"`
}

// Validate reports every way c breaks the command's rules, wrapped in
// [ErrValidation]: the name is non-empty; the interval parses as a Go
// duration of at least [MinRoundInterval]; the limit is at least 1; and the
// starting state the map, factions, and elements make is valid under
// [rules.State.Validate].
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
	if err := c.state().Validate(); err != nil {
		errs = append(errs, err)
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

// state is the exercise's starting state: no objective is held.
func (c CreateExercise) state() rules.State {
	return rules.State{Map: c.Map, Factions: c.Factions, Elements: c.Elements, Holders: map[string]string{}}
}

// Exercise is the umpire's full view of an exercise: its settings, its
// status, the last round resolved, the world as that round left it, the
// verdict once it is over, and when its next round is due while it runs.
// RoundInterval is a Go duration, such as "2s".
type Exercise struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Status        Status         `json:"status"`
	Round         int            `json:"round"`
	RoundLimit    int            `json:"round_limit"`
	RoundInterval string         `json:"round_interval"`
	Factions      [2]string      `json:"factions"`
	State         rules.State    `json:"state"`
	Verdict       *rules.Verdict `json:"verdict"`
	NextRoundAt   *time.Time     `json:"next_round_at"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`

	// intervalMS is RoundInterval in milliseconds, as the database keeps it.
	intervalMS int64
}

// started reports whether the exercise is running or paused: it has
// started and has not yet ended.
func (e Exercise) started() bool {
	return e.Status == StatusRunning || e.Status == StatusPaused
}

// Round is one entry of an exercise's history: the state after the round,
// each faction's observation of it, indexed like the exercise's factions,
// and the verdict. Round 0 is the start.
type Round struct {
	Round        int                  `json:"round"`
	State        rules.State          `json:"state"`
	Observations [2]rules.Observation `json:"observations"`
	Verdict      rules.Verdict        `json:"verdict"`
	ResolvedAt   time.Time            `json:"resolved_at"`
}

// RecordOrders is the command that records one faction's orders for one
// round of an exercise. A reactor's adapter builds it from the orders an
// operations service issued; the domain never sees the event.
type RecordOrders struct {
	Exercise string        `json:"exercise"`
	Faction  string        `json:"faction"`
	Round    int           `json:"round"`
	Orders   []rules.Order `json:"orders"`
}

// Claim is an idempotency claim that a command runs in its transaction
// before it does anything else. It reports whether this is the first time
// the command's input was handled; on false the command changes nothing and
// succeeds. A reactor's adapter binds a Claim over its inbox and the event
// it handles. A caller without an inbox, such as a test, passes a nil
// Claim, which claims nothing.
type Claim func(ctx context.Context, tx *sqlate.Tx) (first bool, err error)
