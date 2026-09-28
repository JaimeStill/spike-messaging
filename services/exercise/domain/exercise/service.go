package exercise

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/standards-lab/sqlate"

	"github.com/JaimeStill/spike-messaging/core/event"
	"github.com/JaimeStill/spike-messaging/services/exercise/data"
	"github.com/JaimeStill/spike-messaging/services/exercise/domain/exercise/rules"
)

// Service is the exercise domain service: the world's commands and the
// umpire's queries. Each command that mutates runs as one transaction
// through [Service.command], so the events it raises commit with the state
// they report.
type Service struct {
	store *store
	rec   *event.Recorder[*sqlate.Tx]
}

// New constructs the service over the service's database and the recorder
// its commands emit through. Construction compiles and binds the
// statements and performs no I/O; a compile failure or a nil recorder is a
// wiring defect and panics.
func New(db *data.Database, rec *event.Recorder[*sqlate.Tx]) *Service {
	if rec == nil {
		panic("exercise: nil recorder")
	}
	return &Service{store: newStore(db), rec: rec}
}

// Verify prepares every statement against the migrated schema; the
// composition root runs it at startup, once the schema is corrected.
func (s *Service) Verify(ctx context.Context) error { return s.store.Verify(ctx) }

// Find returns the umpire's view of the exercise with the id, or
// [ErrNotFound].
func (s *Service) Find(ctx context.Context, id string) (Exercise, error) {
	return s.store.get(ctx, s.store.db, id)
}

// History returns the exercise's rounds, round 0 first, or [ErrNotFound].
// An exercise that has not started has none.
func (s *Service) History(ctx context.Context, id string) ([]Round, error) {
	if _, err := s.Find(ctx, id); err != nil {
		return nil, err
	}
	return s.store.rounds(ctx, id)
}

// Create creates an exercise from c, not yet started: round 0, the
// starting state c describes, and no objective held. It raises nothing, as
// no event reports an exercise before it starts.
func (s *Service) Create(ctx context.Context, c CreateExercise) (Exercise, error) {
	if err := c.Validate(); err != nil {
		return Exercise{}, err
	}
	interval, _ := c.interval()
	return s.command(ctx, func(tx *sqlate.Tx, _ *event.Queue) (Exercise, error) {
		id, err := s.store.insert(ctx, tx, c, interval)
		if err != nil {
			return Exercise{}, err
		}
		return s.store.get(ctx, tx, id)
	})
}

// Start starts a created exercise, or returns [ErrConflict]: it runs, its
// first round due one interval from the database's clock, and round 0, the
// start, is recorded in its history. It raises [Started], then
// [RoundObserved] for each faction's observation of round 0, so the chain
// has its first observation. When the start's own verdict is already over,
// as with a faction that has no element, the exercise concludes at once and
// [Concluded] follows.
func (s *Service) Start(ctx context.Context, id string) (Exercise, error) {
	return s.command(ctx, func(tx *sqlate.Tx, q *event.Queue) (Exercise, error) {
		ex, err := s.store.get(ctx, tx, id)
		if err != nil {
			return Exercise{}, err
		}
		if ex.Status != StatusCreated {
			return Exercise{}, conflict("start", ex)
		}
		obs := rules.Observe(ex.State, 0)
		v := rules.Judge(ex.State, 0, ex.RoundLimit)
		var ok bool
		if v.Over {
			ok, err = s.store.markConcluded(ctx, tx, id, StatusCreated, 0, ex.State, v)
		} else {
			ok, err = s.store.markStarted(ctx, tx, id)
		}
		if err != nil {
			return Exercise{}, err
		}
		if !ok {
			return Exercise{}, moved("start", id)
		}
		if err := s.store.addRound(ctx, tx, id, Round{Round: 0, State: ex.State, Observations: obs, Verdict: v}); err != nil {
			return Exercise{}, err
		}
		raiseStarted(q, ex)
		raiseObserved(q, id, obs)
		if v.Over {
			raiseConcluded(q, id, 0, v)
		}
		return s.store.get(ctx, tx, id)
	})
}

// Pause holds a running exercise's rounds until it resumes, or returns
// [ErrConflict]. It raises nothing.
func (s *Service) Pause(ctx context.Context, id string) (Exercise, error) {
	return s.transition(ctx, id, "pause", StatusRunning, s.store.markPaused)
}

// Resume runs a paused exercise again, its next round due one interval
// from the database's clock, or returns [ErrConflict]. It raises nothing.
func (s *Service) Resume(ctx context.Context, id string) (Exercise, error) {
	return s.transition(ctx, id, "resume", StatusPaused, s.store.markResumed)
}

// transition moves the exercise out of status from by mark, the command
// named verb, which raises nothing.
func (s *Service) transition(
	ctx context.Context,
	id, verb string,
	from Status,
	mark func(context.Context, *sqlate.Tx, string) (bool, error),
) (Exercise, error) {
	return s.command(ctx, func(tx *sqlate.Tx, _ *event.Queue) (Exercise, error) {
		ex, err := s.store.get(ctx, tx, id)
		if err != nil {
			return Exercise{}, err
		}
		if ex.Status != from {
			return Exercise{}, conflict(verb, ex)
		}
		ok, err := mark(ctx, tx, id)
		if err != nil {
			return Exercise{}, err
		}
		if !ok {
			return Exercise{}, moved(verb, id)
		}
		return s.store.get(ctx, tx, id)
	})
}

// Stop ends an exercise that is created, running, or paused before its
// verdict, or returns [ErrConflict]: it is stopped, with a verdict that is
// over, has no winner, and gives "stopped" as its reason. It raises
// [Concluded] only for an exercise that had started, so the services that
// opened it close what they hold; a created exercise was never announced.
func (s *Service) Stop(ctx context.Context, id string) (Exercise, error) {
	return s.command(ctx, func(tx *sqlate.Tx, q *event.Queue) (Exercise, error) {
		ex, err := s.store.get(ctx, tx, id)
		if err != nil {
			return Exercise{}, err
		}
		if ex.Status != StatusCreated && !ex.started() {
			return Exercise{}, conflict("stop", ex)
		}
		v := rules.Verdict{Over: true, Reason: reasonStopped}
		ok, err := s.store.markStopped(ctx, tx, id, ex.Status, v)
		if err != nil {
			return Exercise{}, err
		}
		if !ok {
			return Exercise{}, moved("stop", id)
		}
		if ex.started() {
			raiseConcluded(q, id, ex.Round, v)
		}
		return s.store.get(ctx, tx, id)
	})
}

// RecordOrders records a faction's orders for a round of an exercise,
// replacing any it recorded for that round before. claim, when not nil,
// runs first in the command's transaction, and when it reports a repeat
// the command changes nothing and succeeds, so a redelivered event is
// handled once.
//
// The exercise is read under a shared lock, so the command waits on a
// round being resolved and judges the orders against the round it leaves.
// It refuses, with an error [event.IsPermanent] reports, since no
// redelivery could succeed: an exercise that does not exist
// ([ErrNotFound]); a faction that is not one of its two ([ErrValidation]);
// an exercise that concluded or stopped, or an order for a round already
// resolved ([ErrConflict]). A late order is dropped, so a late faction
// stands still. The refusal rolls the claim back with the rest. It raises
// nothing.
func (s *Service) RecordOrders(ctx context.Context, cmd RecordOrders, claim Claim) error {
	_, err := s.command(ctx, func(tx *sqlate.Tx, _ *event.Queue) (struct{}, error) {
		if claim != nil {
			first, err := claim(ctx, tx)
			if err != nil {
				return struct{}{}, fmt.Errorf("record orders: claim: %w", err)
			}
			if !first {
				return struct{}{}, nil
			}
		}
		ex, err := s.store.share(ctx, tx, cmd.Exercise)
		if errors.Is(err, ErrNotFound) {
			return struct{}{}, event.Permanent(fmt.Errorf("record orders: %w", err))
		}
		if err != nil {
			return struct{}{}, err
		}
		switch {
		case !slices.Contains(ex.Factions[:], cmd.Faction):
			return struct{}{}, event.Permanent(fmt.Errorf("record orders: %w: faction %q is not in exercise %s",
				ErrValidation, cmd.Faction, ex.ID))
		case ex.Status == StatusConcluded || ex.Status == StatusStopped:
			return struct{}{}, event.Permanent(fmt.Errorf("record orders: %w", conflict("record orders for", ex)))
		case cmd.Round <= ex.Round:
			return struct{}{}, event.Permanent(fmt.Errorf("record orders: %w: round %d of exercise %s is already resolved",
				ErrConflict, cmd.Round, ex.ID))
		}
		return struct{}{}, s.store.putOrders(ctx, tx, cmd)
	})
	return err
}

// ResolveDue resolves the next round of every running exercise that is
// due, each in a transaction of its own, and returns how many it resolved.
// An exercise another replica is resolving, or that paused, stopped, or was
// resolved since it was found due, is skipped. One exercise's failure never
// stops the others: the failures are returned joined, each naming its
// exercise.
//
// A round applies the orders both factions recorded for it, an order for
// an element of the other faction ignored, under [rules.Resolve]. The
// exercise advances to the round, its next round due one interval from the
// database's clock, or concludes when the round's verdict is over; the
// round is recorded in its history. It raises [RoundObserved] for each
// faction's observation of the round, then [Concluded] when it concluded.
func (s *Service) ResolveDue(ctx context.Context) (int, error) {
	ids, err := s.store.due(ctx)
	if err != nil {
		return 0, err
	}
	resolved := 0
	var errs []error
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}
		ok, err := s.resolve(ctx, id)
		if err != nil {
			errs = append(errs, fmt.Errorf("resolve exercise %s: %w", id, err))
			continue
		}
		if ok {
			resolved++
		}
	}
	return resolved, errors.Join(errs...)
}

// resolve resolves the next round of the exercise with the id, reporting
// false when it is no longer due or another transaction holds it.
func (s *Service) resolve(ctx context.Context, id string) (bool, error) {
	return s.command(ctx, func(tx *sqlate.Tx, q *event.Queue) (bool, error) {
		ex, ok, err := s.store.lockIfDue(ctx, tx, id)
		if err != nil || !ok {
			return false, err
		}
		round := ex.Round + 1
		recorded, err := s.store.orders(ctx, tx, id, round)
		if err != nil {
			return false, err
		}
		next, obs, v := rules.Resolve(ex.State, round, ex.RoundLimit, ownOrders(ex.State, recorded))
		if v.Over {
			ok, err = s.store.markConcluded(ctx, tx, id, StatusRunning, round, next, v)
		} else {
			ok, err = s.store.markAdvanced(ctx, tx, id, round, next)
		}
		if err != nil {
			return false, err
		}
		if !ok {
			return false, moved("resolve", id)
		}
		if err := s.store.addRound(ctx, tx, id, Round{Round: round, State: next, Observations: obs, Verdict: v}); err != nil {
			return false, err
		}
		raiseObserved(q, id, obs)
		if v.Over {
			raiseConcluded(q, id, round, v)
		}
		return true, nil
	})
}

// ownOrders returns the orders each faction recorded, in the factions'
// order, keeping only those for an element of the faction that issued
// them: a faction commands its own elements alone.
func ownOrders(s rules.State, recorded map[string][]rules.Order) []rules.Order {
	owner := make(map[string]string, len(s.Elements))
	for _, e := range s.Elements {
		owner[e.ID] = e.Faction
	}
	var out []rules.Order
	for _, f := range s.Factions {
		for _, o := range recorded[f] {
			if owner[o.Element] == f {
				out = append(out, o)
			}
		}
	}
	return out
}

// conflict is the error for a command, named verb, that ex's status does
// not allow.
func conflict(verb string, ex Exercise) error {
	return fmt.Errorf("%w: cannot %s exercise %s: it is %s", ErrConflict, verb, ex.ID, ex.Status)
}

// moved is the error for a command, named verb, whose exercise changed
// status between the command's read and its write.
func moved(verb, id string) error {
	return fmt.Errorf("%w: cannot %s exercise %s: its status changed", ErrConflict, verb, id)
}
