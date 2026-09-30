package exercise

import (
	"context"

	"github.com/standards-lab/sqlate"

	"github.com/JaimeStill/spike-messaging/core/event"
	"github.com/JaimeStill/spike-messaging/services/exercise/domain/exercise/rules"
)

// The events the domain raises. Each is about one exercise, and the
// exercise's ID is its subject. An event reports a committed mutation, so a
// command raises it only inside [Service.command].
var (
	// Started reports an exercise that started: its public settings, so a
	// consumer can open its own view of the exercise before any round.
	Started = event.Define[StartedData]("exercise.started")
	// RoundObserved reports what one faction observed after a round. Each
	// round raises one per faction, and the start raises them for round 0.
	RoundObserved = event.Define[ObservedData]("exercise.round.observed")
	// RoundResolved reports what a round's resolution did, as the umpire
	// records it: its engagements, the objectives that changed hands, and
	// the elements destroyed. Each resolved round raises one, before its
	// observations. It reveals every faction's elements, so it is for an
	// observer of the whole exercise, not for a faction's services.
	RoundResolved = event.Define[ResolvedData]("exercise.round.resolved")
	// Concluded reports an exercise that ended, by its verdict or by a stop,
	// so a consumer closes what it holds of it.
	Concluded = event.Define[ConcludedData]("exercise.concluded")
)

// StartedData is the event entity of [Started]: the exercise's public
// settings. The map is public and the elements are not, so it carries no
// element.
type StartedData struct {
	Exercise        string    `json:"exercise"`
	Name            string    `json:"name"`
	Map             rules.Map `json:"map"`
	Factions        [2]string `json:"factions"`
	RoundIntervalMS int64     `json:"round_interval_ms"`
	RoundLimit      int       `json:"round_limit"`
}

// ObservedData is the event entity of [RoundObserved]: one faction's
// observation of one round, with its own elements, the enemy contacts it
// sees, and the objectives in its sight.
type ObservedData struct {
	Exercise   string                  `json:"exercise"`
	Faction    string                  `json:"faction"`
	Round      int                     `json:"round"`
	Own        []rules.Element         `json:"own"`
	Contacts   []rules.Element         `json:"contacts"`
	Objectives []rules.ObjectiveStatus `json:"objectives"`
}

// ResolvedData is the event entity of [RoundResolved]: the round and its
// resolution flattened beside the exercise.
type ResolvedData struct {
	Exercise string `json:"exercise"`
	Round    int    `json:"round"`
	rules.Resolution
}

// ConcludedData is the event entity of [Concluded]: the last round, the
// winning faction ("" for a draw or a stop), and the verdict's reason
// ("stopped" for a stop).
type ConcludedData struct {
	Exercise string `json:"exercise"`
	Round    int    `json:"round"`
	Winner   string `json:"winner"`
	Reason   string `json:"reason"`
}

// startedData builds the started event's entity from the exercise.
func startedData(ex Exercise) StartedData {
	return StartedData{
		Exercise:        ex.ID,
		Name:            ex.Name,
		Map:             ex.State.Map,
		Factions:        ex.Factions,
		RoundIntervalMS: ex.intervalMS,
		RoundLimit:      ex.RoundLimit,
	}
}

// observedData builds the observed event's entity for each faction's
// observation of one round of exercise id.
func observedData(id string, obs [2]rules.Observation) [2]ObservedData {
	var out [2]ObservedData
	for i, o := range obs {
		out[i] = ObservedData{
			Exercise:   id,
			Faction:    o.Faction,
			Round:      o.Round,
			Own:        o.Own,
			Contacts:   o.Contacts,
			Objectives: o.Objectives,
		}
	}
	return out
}

// concludedData builds the concluded event's entity from the verdict after
// round of exercise id.
func concludedData(id string, round int, v rules.Verdict) ConcludedData {
	return ConcludedData{Exercise: id, Round: round, Winner: v.Winner, Reason: v.Reason}
}

// raiseStarted raises the started event for ex.
func raiseStarted(q *event.Queue, ex Exercise) {
	Started.Raise(q, ex.ID, startedData(ex))
}

// raiseObserved raises one observed event per faction, in the factions'
// order, for one round of exercise id.
func raiseObserved(q *event.Queue, id string, obs [2]rules.Observation) {
	for _, d := range observedData(id, obs) {
		RoundObserved.Raise(q, id, d)
	}
}

// raiseResolved raises the resolved event for round of exercise id.
func raiseResolved(q *event.Queue, id string, round int, res rules.Resolution) {
	RoundResolved.Raise(q, id, ResolvedData{Exercise: id, Round: round, Resolution: res})
}

// raiseConcluded raises the concluded event for exercise id, ended after
// round by v.
func raiseConcluded(q *event.Queue, id string, round int, v rules.Verdict) {
	Concluded.Raise(q, id, concludedData(id, round, v))
}

// command runs fn, a mutating command's body, as one transaction. When fn
// succeeds, the recorder writes the events fn raised into the same
// transaction, so the state and the events that report it commit together
// or not at all. Every command that mutates runs through command.
func (s *Service) command[R any](ctx context.Context, fn func(*sqlate.Tx, *event.Queue) (R, error)) (R, error) {
	return sqlate.Transact(ctx, s.store.db.DB, s.rec.Emit(ctx, fn))
}
