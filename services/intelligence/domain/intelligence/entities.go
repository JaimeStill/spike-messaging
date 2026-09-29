package intelligence

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/standards-lab/sqlate"

	"github.com/JaimeStill/spike-messaging/services/intelligence/domain/intelligence/fusion"
)

// ErrValidation classifies a command input that the domain rejects because
// of its own fields. The returned error wraps it with every reason found.
var ErrValidation = errors.New("invalid command")

// ErrNotFound reports an exercise for which the service has opened no
// assessment. It wraps sql.ErrNoRows, so a caller that tests for either
// finds it.
var ErrNotFound = fmt.Errorf("assessment not found: %w", sql.ErrNoRows)

// ErrNotOpen reports an input for a faction's assessment that is not open
// yet, as when a round's observation is handled before the start that opens
// it. It is not permanent: the broker redelivers the input, and the start
// has opened the assessment by then.
var ErrNotOpen = errors.New("the assessment is not open yet")

// Status is where a faction's assessment is in its life.
type Status string

// The statuses of an assessment.
const (
	// StatusOpen is an assessment that fuses its faction's observations.
	StatusOpen Status = "open"
	// StatusClosed is an assessment whose exercise concluded.
	StatusClosed Status = "closed"
)

// Assessment is one faction's assessment in an exercise, as the query shows
// it: the picture its observations fused into, flattened beside the
// exercise, the faction, and the status. A closed assessment keeps the
// round its exercise concluded after.
type Assessment struct {
	Exercise string `json:"exercise"`
	Faction  string `json:"faction"`
	Status   Status `json:"status"`
	fusion.Picture
	UpdatedAt   time.Time `json:"updated_at"`
	closedRound int
}

// Open is the open command's input, intelligence's reading of an
// exercise.started event: the exercise, its public map, and its two
// factions.
type Open struct {
	Exercise string     `json:"exercise"`
	Map      fusion.Map `json:"map"`
	Factions [2]string  `json:"factions"`
}

// Validate reports every way c is unusable, wrapping [ErrValidation].
func (c Open) Validate() error {
	var errs []error
	errs = append(errs, checkExercise(c.Exercise))
	for _, f := range c.Factions {
		if f == "" {
			errs = append(errs, errors.New("a faction is empty"))
		}
	}
	if c.Factions[0] == c.Factions[1] {
		errs = append(errs, errors.New("the factions are the same"))
	}
	return invalid(errs)
}

// Observe is the observe command's input, intelligence's reading of an
// exercise.round.observed event: a faction's observation of a round,
// flattened beside the exercise and the faction.
type Observe struct {
	Exercise string `json:"exercise"`
	Faction  string `json:"faction"`
	fusion.Observation
}

// Validate reports every way c is unusable, wrapping [ErrValidation].
func (c Observe) Validate() error {
	return invalid([]error{checkExercise(c.Exercise), checkFaction(c.Faction), checkRound(c.Round)})
}

// Close is the close command's input, intelligence's reading of an
// exercise.concluded event: the exercise, and the round it concluded after.
type Close struct {
	Exercise string `json:"exercise"`
	Round    int    `json:"round"`
}

// Validate reports every way c is unusable, wrapping [ErrValidation].
func (c Close) Validate() error {
	return invalid([]error{checkExercise(c.Exercise), checkRound(c.Round)})
}

// Claim is an idempotency claim that a command runs in its transaction
// before it does anything else. It reports whether this is the first time
// the command's input was handled. On false the command changes nothing and
// succeeds. A reactor's adapter binds a Claim over its inbox and the event
// it handles. A caller without an inbox, such as a test, passes a nil
// Claim, which claims nothing. It is an alias, so the claim a consumer
// built by messaging's Consume hands over is one.
type Claim = func(ctx context.Context, tx *sqlate.Tx) (first bool, err error)

func checkExercise(id string) error {
	if _, err := uuid.Parse(id); err != nil {
		return fmt.Errorf("exercise %q is not a UUID", id)
	}
	return nil
}

func checkFaction(f string) error {
	if f == "" {
		return errors.New("the faction is empty")
	}
	return nil
}

func checkRound(r int) error {
	if r < 0 {
		return fmt.Errorf("round %d is negative", r)
	}
	return nil
}

// invalid joins the reasons found, or returns nil for none.
func invalid(errs []error) error {
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("%w: %w", ErrValidation, err)
	}
	return nil
}
