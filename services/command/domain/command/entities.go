package command

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/standards-lab/sqlate"

	"github.com/JaimeStill/spike-messaging/services/command/domain/command/decide"
)

// ErrValidation classifies a command input that the domain rejects because
// of its own fields. The returned error wraps it with every reason found.
var ErrValidation = errors.New("invalid command")

// ErrNotFound reports an exercise for which the service has opened no
// direction. It wraps sql.ErrNoRows, so a caller that tests for either
// finds it.
var ErrNotFound = fmt.Errorf("direction not found: %w", sql.ErrNoRows)

// ErrNotOpen reports an input for a faction's direction that is not open
// yet, as when a round's assessment is handled before the start that opens
// it. It is not permanent: the broker redelivers the input, and the start
// has opened the direction by then.
var ErrNotOpen = errors.New("the direction is not open yet")

// Status is where a faction's direction is in its life.
type Status string

// The statuses of a direction.
const (
	// StatusOpen is a direction that decides on its faction's assessments.
	StatusOpen Status = "open"
	// StatusClosed is a direction whose exercise concluded.
	StatusClosed Status = "closed"
)

// Direction is one faction's direction in an exercise, as the query shows
// it: the last round it decided on, -1 before the first; the revision of
// the assessment it decided on, 0 before the first; the sequence of the
// last directive it issued, 0 before the first; and the decision standing
// for each of the faction's live elements. A closed direction keeps the
// round its exercise concluded after.
type Direction struct {
	Exercise    string            `json:"exercise"`
	Faction     string            `json:"faction"`
	Status      Status            `json:"status"`
	Round       int               `json:"round"`
	Revision    int               `json:"revision"`
	Sequence    int               `json:"sequence"`
	Decisions   []decide.Decision `json:"decisions"`
	UpdatedAt   time.Time         `json:"updated_at"`
	plan        decide.Map
	closedRound int
}

// Open is the open command's input, command's reading of an
// exercise.started event: the exercise, its public map, and its two
// factions.
type Open struct {
	Exercise string     `json:"exercise"`
	Map      decide.Map `json:"map"`
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

// Decide is the decide command's input, command's reading of an
// intelligence.assessment.issued event: a faction's assessment of a round,
// flattened beside the exercise, the faction, and the assessment's
// revision. Intelligence numbers each assessment it issues for a faction in
// an exercise from 1 up, so a higher revision is a newer assessment.
type Decide struct {
	Exercise string `json:"exercise"`
	Faction  string `json:"faction"`
	Revision int    `json:"revision"`
	decide.Assessment
}

// Validate reports every way c is unusable, wrapping [ErrValidation].
func (c Decide) Validate() error {
	return invalid([]error{
		checkExercise(c.Exercise), checkFaction(c.Faction), checkRound(c.Round), checkCounter("revision", c.Revision),
	})
}

// Close is the close command's input, command's reading of an
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
// succeeds. The consuming reactor binds a Claim over its inbox and the
// event it handles. A caller without an inbox, such as a test, passes a nil
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

func checkCounter(name string, n int) error {
	if n < 1 {
		return fmt.Errorf("%s %d is not positive", name, n)
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
