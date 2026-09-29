package operations

import (
	"context"

	"github.com/standards-lab/sqlate"

	"github.com/JaimeStill/spike-messaging/core/event"
	"github.com/JaimeStill/spike-messaging/services/operations/domain/operations/route"
)

// OrdersIssued reports a faction's orders for a round: the steps that carry
// its elements toward their targets. Its subject is the exercise's ID.
// Maneuver raises one per faction per observed round, for the round after
// it, orders or none; Assign raises it again for the same round when a
// directive changes the plan, and exercise keeps the last it records.
var OrdersIssued = event.Define[OrdersData]("operations.orders.issued")

// OrdersData is the event entity of [OrdersIssued]: the exercise, the
// faction, the round the orders are for, and each ordered element's steps.
type OrdersData struct {
	Exercise string        `json:"exercise"`
	Faction  string        `json:"faction"`
	Round    int           `json:"round"`
	Orders   []route.Order `json:"orders"`
}

// raiseOrders raises the faction's orders for round of exercise id.
func raiseOrders(q *event.Queue, id, faction string, round int, orders []route.Order) {
	if orders == nil {
		orders = []route.Order{}
	}
	OrdersIssued.Raise(q, id, OrdersData{Exercise: id, Faction: faction, Round: round, Orders: orders})
}

// command runs fn, a mutating command's body, as one transaction. When fn
// succeeds, the recorder writes the events fn raised into the same
// transaction, so the state and the events that report it commit together
// or not at all. Every command that mutates runs through command.
func (s *Service) command[R any](ctx context.Context, fn func(*sqlate.Tx, *event.Queue) (R, error)) (R, error) {
	return sqlate.Transact(ctx, s.store.db.DB, s.rec.Emit(ctx, fn))
}
