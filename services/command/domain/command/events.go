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
// faction, the round whose assessment was decided on, the directive's
// sequence, and a directive for each live element. The sequence numbers
// the faction's directives in the exercise from 1 up, so a consumer skips a
// directive whose sequence is no higher than the last it applied, which
// arrived out of order. A directive gives its element's target, null for a
// hold, the rule that chose it, and, for a retreat, pursue, engage, or
// reinforce, the contact.
type DirectiveData struct {
	Exercise   string            `json:"exercise"`
	Faction    string            `json:"faction"`
	Round      int               `json:"round"`
	Sequence   int               `json:"sequence"`
	Directives []decide.Decision `json:"directives"`
}

// raiseDirective raises [DirectiveIssued] with d's decisions for d's round,
// under d's sequence.
func raiseDirective(q *event.Queue, d Direction) {
	DirectiveIssued.Raise(q, d.Exercise, DirectiveData{
		Exercise: d.Exercise, Faction: d.Faction, Round: d.Round, Sequence: d.Sequence, Directives: d.Decisions,
	})
}

// command runs fn, a mutating command's body, as one transaction. When fn
// succeeds, the recorder writes the events fn raised into the same
// transaction, so the state and the events that report it commit together
// or not at all. Every command that mutates runs through command.
func (s *Service) command[R any](ctx context.Context, fn func(*sqlate.Tx, *event.Queue) (R, error)) (R, error) {
	return sqlate.Transact(ctx, s.store.db.DB, s.rec.Emit(ctx, fn))
}
