package operations

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/standards-lab/sqlate"

	"github.com/JaimeStill/spike-messaging/core/event"
	"github.com/JaimeStill/spike-messaging/services/operations/data"
	"github.com/JaimeStill/spike-messaging/services/operations/domain/operations/route"
)

// Service is the operations domain service. It holds the commands the
// reactors invoke and the query the API serves. Each command that mutates
// runs as one transaction through [Service.command], so the orders it
// raises commit with the state they report.
type Service struct {
	store *store
	rec   *event.Recorder[*sqlate.Tx]
}

// New constructs the service over the operations database and the recorder
// its commands emit through. New compiles and binds the statements and
// performs no I/O. A compile failure or a nil recorder is a wiring defect,
// so New panics.
func New(db *data.Database, rec *event.Recorder[*sqlate.Tx]) *Service {
	if rec == nil {
		panic("operations: nil recorder")
	}
	return &Service{store: newStore(db), rec: rec}
}

// Verify prepares every statement against the migrated schema. The
// composition root runs it at startup, after the migrations.
func (s *Service) Verify(ctx context.Context) error { return s.store.Verify(ctx) }

// Find returns every faction's operation in the exercise, by faction, or
// [ErrNotFound].
func (s *Service) Find(ctx context.Context, exercise string) ([]Operation, error) {
	if checkExercise(exercise) != nil {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, exercise)
	}
	ops, err := s.store.all(ctx, exercise)
	if err != nil {
		return nil, err
	}
	if len(ops) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, exercise)
	}
	return ops, nil
}

// Open opens both factions' operations in an exercise, over its map. An
// operation already open is left as it is. It raises nothing.
func (s *Service) Open(ctx context.Context, c Open, claim Claim) error {
	if err := c.Validate(); err != nil {
		return event.Permanent(fmt.Errorf("open: %w", err))
	}
	return s.claimed(ctx, "open", claim, func(tx *sqlate.Tx, _ *event.Queue) error {
		for _, f := range c.Factions {
			if err := s.store.insert(ctx, tx, c.Exercise, f, c.Map, c.RoundLimit); err != nil {
				return err
			}
		}
		return nil
	})
}

// Assign sets the targets a faction's directives name, each with its rule:
// an element's target, or none, which holds it. Each directive carries its
// faction's whole target state, so Assign applies the newest whenever it
// arrives, even one decided on a round before the last the operation acted
// on, as after an outage; it skips only a directive whose sequence is no
// higher than the last it applied, which a redelivery or a late arrival
// behind a newer one is, and any for a closed operation.
//
// A directive decided on the round the operation last acted on, which
// changes its plan, raises [OrdersIssued] again for the round those orders
// were for, so the new directives take effect without waiting a round. A
// directive on a later round, which overtook that round's observation,
// only sets the targets the observation's Maneuver plans with.
func (s *Service) Assign(ctx context.Context, c Assign, claim Claim) error {
	if err := c.Validate(); err != nil {
		return event.Permanent(fmt.Errorf("assign: %w", err))
	}
	return s.claimed(ctx, "assign", claim, func(tx *sqlate.Tx, q *event.Queue) error {
		op, err := s.store.lock(ctx, tx, c.Exercise, c.Faction)
		if err != nil || op.Status == StatusClosed || c.Sequence <= op.DirectiveSeq {
			return err
		}
		op.DirectiveSeq = c.Sequence
		before := op.orders()
		op.Standing = maps.Clone(op.Standing)
		if op.Standing == nil {
			op.Standing = map[string]route.Standing{}
		}
		for _, d := range c.Directives {
			if d.Target == nil {
				delete(op.Standing, d.Element)
			} else {
				op.Standing[d.Element] = route.Standing{Target: *d.Target, Rule: d.Rule}
			}
		}
		// Only a directive on the round the operation last acted on can
		// reshape orders still to come: one on a later round overtook that
		// round's observation, whose Maneuver plans with its targets, and the
		// orders out now are for a round already resolved.
		if after := op.orders(); c.Round == op.LastRound && !sameOrders(before, after) {
			op.raise(q, after)
		}
		return s.store.save(ctx, tx, op)
	})
}

// Maneuver takes a faction's observation of a round: it records the
// faction's live elements, drops the targets and rules of those destroyed,
// and raises [OrdersIssued] for the next round, the steps that carry each
// element toward its target. It raises the event even when no element moves,
// and raises none for a round past the exercise's round limit. It skips an
// observation of a round the operation has already acted on, and any for a
// closed operation.
func (s *Service) Maneuver(ctx context.Context, c Maneuver, claim Claim) error {
	if err := c.Validate(); err != nil {
		return event.Permanent(fmt.Errorf("maneuver: %w", err))
	}
	return s.claimed(ctx, "maneuver", claim, func(tx *sqlate.Tx, q *event.Queue) error {
		op, err := s.store.lock(ctx, tx, c.Exercise, c.Faction)
		if err != nil || op.Status == StatusClosed || c.Round <= op.LastRound {
			return err
		}
		op.Elements = c.Own
		live := func(id string) bool {
			return slices.ContainsFunc(op.Elements, func(e route.Element) bool { return e.ID == id })
		}
		op.Standing = maps.Clone(op.Standing)
		maps.DeleteFunc(op.Standing, func(id string, _ route.Standing) bool { return !live(id) })
		op.LastRound = c.Round
		op.raise(q, op.orders())
		return s.store.save(ctx, tx, op)
	})
}

// Close closes every faction's operation in an exercise that concluded, so
// no later input acts on it. It raises nothing.
func (s *Service) Close(ctx context.Context, c Close, claim Claim) error {
	if err := c.Validate(); err != nil {
		return event.Permanent(fmt.Errorf("close: %w", err))
	}
	return s.claimed(ctx, "close", claim, func(tx *sqlate.Tx, _ *event.Queue) error {
		n, err := s.store.closeAll(ctx, tx, c.Exercise)
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

// orders plans the operation's orders for the round after its last.
func (op Operation) orders() []route.Order {
	return route.Plan(op.plan, op.Elements, op.Standing)
}

// raise raises the operation's orders for the round after its last, unless
// that round is past the exercise's round limit.
func (op Operation) raise(q *event.Queue, orders []route.Order) {
	if next := op.LastRound + 1; next <= op.limit {
		raiseOrders(q, op.Exercise, op.Faction, next, orders)
	}
}

// sameOrders reports whether a and b order the same steps, each a retreat
// or a pursuit, or neither, alike.
func sameOrders(a, b []route.Order) bool {
	return slices.EqualFunc(a, b, func(x, y route.Order) bool {
		return x.Element == y.Element && x.Retreat == y.Retreat && x.Pursue == y.Pursue && slices.Equal(x.Steps, y.Steps)
	})
}
