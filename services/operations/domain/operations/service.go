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

// New constructs the service over the service's database and the recorder
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

// Assign sets the targets a faction's directives name: an element's target,
// or none, which holds it. It skips directives decided on a round earlier
// than the last the operation acted on, and any for a closed operation.
// Once the operation has issued orders, a change to its plan raises
// [OrdersIssued] again for the round those orders were for, so the new
// directives take effect without waiting a round.
func (s *Service) Assign(ctx context.Context, c Assign, claim Claim) error {
	if err := c.Validate(); err != nil {
		return event.Permanent(fmt.Errorf("assign: %w", err))
	}
	return s.claimed(ctx, "assign", claim, func(tx *sqlate.Tx, q *event.Queue) error {
		op, err := s.store.lock(ctx, tx, c.Exercise, c.Faction)
		if err != nil || op.Status == StatusClosed || c.Round < op.LastRound {
			return err
		}
		before := op.orders()
		op.Targets = maps.Clone(op.Targets)
		if op.Targets == nil {
			op.Targets = map[string]route.Location{}
		}
		for _, d := range c.Directives {
			if d.Target == nil {
				delete(op.Targets, d.Element)
			} else {
				op.Targets[d.Element] = *d.Target
			}
		}
		if after := op.orders(); op.LastRound >= 0 && !sameOrders(before, after) {
			op.raise(q, after)
		}
		return s.store.save(ctx, tx, op)
	})
}

// Maneuver takes a faction's observation of a round: it records the
// faction's live elements, drops the targets of those destroyed, and raises
// [OrdersIssued] for the next round, the steps that carry each element
// toward its target. It raises the event even when no element moves, and
// raises none for a round past the exercise's round limit. It skips an
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
		op.Targets = maps.Clone(op.Targets)
		maps.DeleteFunc(op.Targets, func(id string, _ route.Location) bool {
			return !slices.ContainsFunc(op.Elements, func(e route.Element) bool { return e.ID == id })
		})
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
	return route.Plan(op.plan, op.Elements, op.Targets)
}

// raise raises the operation's orders for the round after its last, unless
// that round is past the exercise's round limit.
func (op Operation) raise(q *event.Queue, orders []route.Order) {
	if next := op.LastRound + 1; next <= op.limit {
		raiseOrders(q, op.Exercise, op.Faction, next, orders)
	}
}

// sameOrders reports whether a and b order the same steps.
func sameOrders(a, b []route.Order) bool {
	return slices.EqualFunc(a, b, func(x, y route.Order) bool {
		return x.Element == y.Element && slices.Equal(x.Steps, y.Steps)
	})
}
