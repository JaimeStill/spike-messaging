package command

import (
	"context"

	"github.com/standards-lab/sqlate"

	"github.com/JaimeStill/spike-messaging/core/event"
	"github.com/JaimeStill/spike-messaging/services/command/domain/command/decide"
)

// DirectiveIssued reports a faction's directives, decided on its assessment
// of a round, when the decision changed where any of its elements heads or
// the rule that sends it there. Its subject is the exercise's ID. It lists
// every live element, so it carries the faction's whole target state, and a
// consumer that skipped an earlier directive loses nothing.
var DirectiveIssued = event.Define[DirectiveData]("command.directive.issued")

// DirectiveData is the event entity of [DirectiveIssued]: the exercise, the
// faction, the round whose assessment was decided on, and a directive for
// each live element. A directive gives its element's target, null for a
// hold, the rule that chose it, and for a retreat, an engage, or a
// reinforce the contact.
type DirectiveData struct {
	Exercise   string            `json:"exercise"`
	Faction    string            `json:"faction"`
	Round      int               `json:"round"`
	Directives []decide.Decision `json:"directives"`
}

// raiseDirective raises [DirectiveIssued] with d's decisions for d's round.
func raiseDirective(q *event.Queue, d Direction) {
	DirectiveIssued.Raise(q, d.Exercise, DirectiveData{
		Exercise: d.Exercise, Faction: d.Faction, Round: d.Round, Directives: d.Decisions,
	})
}

// command runs fn, a mutating command's body, as one transaction. When fn
// succeeds, the recorder writes the events fn raised into the same
// transaction, so the state and the events that report it commit together
// or not at all. Every command that mutates runs through command.
func (s *Service) command[R any](ctx context.Context, fn func(*sqlate.Tx, *event.Queue) (R, error)) (R, error) {
	return sqlate.Transact(ctx, s.store.db.DB, s.rec.Emit(ctx, fn))
}
