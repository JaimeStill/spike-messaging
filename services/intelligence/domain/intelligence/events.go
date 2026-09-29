package intelligence

import (
	"context"

	"github.com/standards-lab/sqlate"

	"github.com/JaimeStill/spike-messaging/core/event"
	"github.com/JaimeStill/spike-messaging/services/intelligence/domain/intelligence/fusion"
)

// AssessmentIssued reports a faction's assessment of an observed round:
// what the faction knows once that round's observation is fused into what
// it knew. Its subject is the exercise's ID. Observe raises one per faction
// per observed round.
var AssessmentIssued = event.Define[AssessmentData]("intelligence.assessment.issued")

// AssessmentData is the event entity of [AssessmentIssued]: the exercise,
// the faction, and its picture flattened beside them, which is the round
// assessed, the faction's own elements, the contacts it knows of, each with
// the round it was last seen and its age, and every objective, with its
// last-seen holder when known.
type AssessmentData struct {
	Exercise string `json:"exercise"`
	Faction  string `json:"faction"`
	fusion.Picture
}

// raiseAssessment raises the faction's assessment, its picture p, of
// exercise id.
func raiseAssessment(q *event.Queue, id, faction string, p fusion.Picture) {
	AssessmentIssued.Raise(q, id, AssessmentData{Exercise: id, Faction: faction, Picture: p})
}

// command runs fn, a mutating command's body, as one transaction. When fn
// succeeds, the recorder writes the events fn raised into the same
// transaction, so the state and the events that report it commit together
// or not at all. Every command that mutates runs through command.
func (s *Service) command[R any](ctx context.Context, fn func(*sqlate.Tx, *event.Queue) (R, error)) (R, error) {
	return sqlate.Transact(ctx, s.store.db.DB, s.rec.Emit(ctx, fn))
}
