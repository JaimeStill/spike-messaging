package intelligence

import (
	"context"
	"errors"
	"fmt"

	"github.com/standards-lab/sqlate"

	"github.com/JaimeStill/spike-messaging/core/event"
	"github.com/JaimeStill/spike-messaging/services/intelligence/data"
	"github.com/JaimeStill/spike-messaging/services/intelligence/domain/intelligence/fusion"
)

// Service is the intelligence domain service. It holds the commands the
// reactors invoke and the query the API serves. Each command that mutates
// runs as one transaction through [Service.command], so the assessments it
// raises commit with the state they report.
type Service struct {
	store         *store
	rec           *event.Recorder[*sqlate.Tx]
	contactRounds int
}

// New constructs the service over the intelligence database and the
// recorder its commands emit through. contactRounds is how many rounds a
// contact stays in an assessment unseen before it drops. New compiles and
// binds the statements and performs no I/O. A compile failure, a nil
// recorder, or contactRounds below 1 is a wiring defect, so New panics.
func New(db *data.Database, rec *event.Recorder[*sqlate.Tx], contactRounds int) *Service {
	if rec == nil {
		panic("intelligence: nil recorder")
	}
	if contactRounds < 1 {
		panic(fmt.Sprintf("intelligence: contact rounds %d is below 1", contactRounds))
	}
	return &Service{store: newStore(db), rec: rec, contactRounds: contactRounds}
}

// Verify prepares every statement against the migrated schema. The
// composition root runs it at startup, after the migrations.
func (s *Service) Verify(ctx context.Context) error { return s.store.Verify(ctx) }

// Find returns every faction's assessment in the exercise, by faction, or
// [ErrNotFound].
func (s *Service) Find(ctx context.Context, exercise string) ([]Assessment, error) {
	if checkExercise(exercise) != nil {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, exercise)
	}
	as, err := s.store.all(ctx, exercise)
	if err != nil {
		return nil, err
	}
	if len(as) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, exercise)
	}
	return as, nil
}

// Open opens both factions' assessments in an exercise, each knowing the
// map's objectives and none of their holders. An assessment already open is
// left as it is. It raises nothing.
func (s *Service) Open(ctx context.Context, c Open, claim Claim) error {
	if err := c.Validate(); err != nil {
		return event.Permanent(fmt.Errorf("open: %w", err))
	}
	p := fusion.Open(c.Map.Objectives())
	return s.claimed(ctx, "open", claim, func(tx *sqlate.Tx, _ *event.Queue) error {
		for _, f := range c.Factions {
			if err := s.store.insert(ctx, tx, c.Exercise, f, p); err != nil {
				return err
			}
		}
		return nil
	})
}

// Observe fuses a faction's observation of a round into its assessment and
// raises [AssessmentIssued] with the result. It skips an observation of a
// round the assessment already covers, and one of a closed assessment past
// the round its exercise concluded after. exercise raises the final round's
// observations with the conclusion, and they arrive on separate
// subscriptions, so the close can be handled first; the final round is
// still assessed.
func (s *Service) Observe(ctx context.Context, c Observe, claim Claim) error {
	if err := c.Validate(); err != nil {
		return event.Permanent(fmt.Errorf("observe: %w", err))
	}
	return s.claimed(ctx, "observe", claim, func(tx *sqlate.Tx, q *event.Queue) error {
		a, err := s.store.lock(ctx, tx, c.Exercise, c.Faction)
		if err != nil || c.Round <= a.Round || a.Status == StatusClosed && c.Round > a.closedRound {
			return err
		}
		a.Picture = fusion.Fuse(a.Picture, c.Observation, s.contactRounds)
		raiseAssessment(q, a.Exercise, a.Faction, a.Picture)
		return s.store.save(ctx, tx, a)
	})
}

// Close closes every faction's assessment in an exercise that concluded,
// recording the round it concluded after, so no observation of a later
// round changes it. It raises nothing.
func (s *Service) Close(ctx context.Context, c Close, claim Claim) error {
	if err := c.Validate(); err != nil {
		return event.Permanent(fmt.Errorf("close: %w", err))
	}
	return s.claimed(ctx, "close", claim, func(tx *sqlate.Tx, _ *event.Queue) error {
		n, err := s.store.closeAll(ctx, tx, c.Exercise, c.Round)
		if err == nil && n == 0 {
			err = fmt.Errorf("%w: %s", ErrNotOpen, c.Exercise)
		}
		return err
	})
}

// claimed runs fn as a command, after claim when there is one: when the
// claim reports a repeat, the command changes nothing and succeeds, so a
// redelivered event is handled once. A failure of fn rolls the claim back
// with it, so a redelivery claims again.
func (s *Service) claimed(ctx context.Context, name string, claim Claim, fn func(*sqlate.Tx, *event.Queue) error) error {
	_, err := s.command(ctx, func(tx *sqlate.Tx, q *event.Queue) (struct{}, error) {
		if claim != nil {
			first, err := claim(ctx, tx)
			if err != nil {
				return struct{}{}, fmt.Errorf("claim: %w", err)
			}
			if !first {
				return struct{}{}, nil
			}
		}
		return struct{}{}, fn(tx, q)
	})
	if err != nil && !errors.Is(err, ErrNotOpen) {
		return fmt.Errorf("%s: %w", name, err)
	}
	return err
}
