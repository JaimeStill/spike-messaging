package command

import (
	"context"
	"errors"
	"fmt"

	"github.com/standards-lab/sqlate"

	"github.com/JaimeStill/spike-messaging/core/event"
	"github.com/JaimeStill/spike-messaging/services/command/data"
	"github.com/JaimeStill/spike-messaging/services/command/domain/command/decide"
)

// Service is the command domain service. It holds the commands the
// reactors invoke and the query the API serves. Each command that mutates
// runs as one transaction through [Service.command], so the directives it
// raises commit with the decisions they report.
type Service struct {
	store *store
	rec   *event.Recorder[*sqlate.Tx]
}

// New constructs the service over the command database and the recorder its
// commands emit through. New compiles and binds the statements and performs
// no I/O. A compile failure or a nil recorder is a wiring defect, so New
// panics.
func New(db *data.Database, rec *event.Recorder[*sqlate.Tx]) *Service {
	if rec == nil {
		panic("command: nil recorder")
	}
	return &Service{store: newStore(db), rec: rec}
}

// Verify prepares every statement against the migrated schema. The
// composition root runs it at startup, after the migrations.
func (s *Service) Verify(ctx context.Context) error { return s.store.Verify(ctx) }

// Find returns every faction's direction in the exercise, by faction, or
// [ErrNotFound].
func (s *Service) Find(ctx context.Context, exercise string) ([]Direction, error) {
	if checkExercise(exercise) != nil {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, exercise)
	}
	ds, err := s.store.all(ctx, exercise)
	if err != nil {
		return nil, err
	}
	if len(ds) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, exercise)
	}
	return ds, nil
}

// Open opens both factions' directions in an exercise over its map, with
// no decision yet. A direction already open is left as it is. It raises
// nothing.
func (s *Service) Open(ctx context.Context, c Open, claim Claim) error {
	if err := c.Validate(); err != nil {
		return event.Permanent(fmt.Errorf("open: %w", err))
	}
	return s.claimed(ctx, "open", claim, func(tx *sqlate.Tx, _ *event.Queue) error {
		for _, f := range c.Factions {
			if err := s.store.insert(ctx, tx, c.Exercise, f, c.Map); err != nil {
				return err
			}
		}
		return nil
	})
}

// Decide decides on a faction's assessment of a round by [decide.Decide],
// over the decisions standing, and records the result. It raises
// [DirectiveIssued] with every live element's decision when any element's
// target changed; a changed rule alone, or a destroyed element, changes no
// target. It skips an assessment of a round the direction already decided
// on, and one of a closed direction past the round its exercise concluded
// after. A final round's assessment arrives on a subscription apart from
// the conclusion, and can be handled after the close; the final round is
// still decided on.
func (s *Service) Decide(ctx context.Context, c Decide, claim Claim) error {
	if err := c.Validate(); err != nil {
		return event.Permanent(fmt.Errorf("decide: %w", err))
	}
	return s.claimed(ctx, "decide", claim, func(tx *sqlate.Tx, q *event.Queue) error {
		d, err := s.store.lock(ctx, tx, c.Exercise, c.Faction)
		if err != nil || c.Round <= d.Round || d.Status == StatusClosed && c.Round > d.closedRound {
			return err
		}
		next := decide.Decide(d.plan, d.Faction, c.Assessment, d.Decisions)
		changed := retargets(d.Decisions, next)
		d.Round, d.Decisions = c.Round, next
		if changed {
			raiseDirective(q, d)
		}
		return s.store.save(ctx, tx, d)
	})
}

// Close closes every faction's direction in an exercise that concluded,
// recording the round it concluded after, so no assessment of a later round
// changes it. It raises nothing.
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

// retargets reports whether next sends any element somewhere prev did not:
// a different target, or a target where it held. An element with no
// decision in prev held.
func retargets(prev, next []decide.Decision) bool {
	was := make(map[string]*decide.Location, len(prev))
	for _, d := range prev {
		was[d.Element] = d.Target
	}
	for _, d := range next {
		before := was[d.Element]
		if (before == nil) != (d.Target == nil) || before != nil && *before != *d.Target {
			return true
		}
	}
	return false
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
