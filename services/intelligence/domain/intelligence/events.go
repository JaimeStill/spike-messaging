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
// per observed round, and Alert one when it changes a picture that already
// covers the round it reports.
var AssessmentIssued = event.Define[AssessmentData]("intelligence.assessment.issued")

// AssessmentData is the event entity of [AssessmentIssued]: the exercise, the
// faction, the assessment's revision, and the faction's picture flattened
// beside them. The revision counts the assessments issued for the faction,
// from 1, across Observe and Alert; an observation's assessment and an
// alert's of one round differ in it, and a consumer decides only an
// assessment with a higher revision than the last it decided. The picture
// gives the round assessed, the faction's own elements, its contacts, each
// with the round it was last seen and its age, the objectives it has seen,
// each with its last-seen holder, and the cells its elements have had in
// sight.
type AssessmentData struct {
	Exercise string `json:"exercise"`
	Faction  string `json:"faction"`
	Revision int    `json:"revision"`
	fusion.Picture
}

// raiseAssessment advances a's revision and raises its assessment. The
// caller saves a, which records the revision.
func raiseAssessment(q *event.Queue, a *Assessment) {
	a.Revision++
	AssessmentIssued.Raise(q, a.Exercise, AssessmentData{
		Exercise: a.Exercise, Faction: a.Faction, Revision: a.Revision, Picture: a.Picture,
	})
}

// command runs fn, a mutating command's body, as one transaction. When fn
// succeeds, the recorder writes the events fn raised into the same
// transaction, so the state and the events that report it commit together
// or not at all. Every command that mutates runs through command.
func (s *Service) command[R any](ctx context.Context, fn func(*sqlate.Tx, *event.Queue) (R, error)) (R, error) {
	return sqlate.Transact(ctx, s.store.db.DB, s.rec.Emit(ctx, fn))
}
