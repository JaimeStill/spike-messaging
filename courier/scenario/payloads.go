package scenario

import (
	"encoding/json"

	"github.com/JaimeStill/spike-messaging/core/event"
)

// The events the join scenarios read, and the directive the directives
// scenario issues: exercise's start, its observations of each faction, its
// record of each round, its alert to a faction that lost an objective, and
// its conclusion; intelligence's assessments; command's directives; and
// operations' orders.
const (
	startedType    = "exercise.started"
	observedType   = "exercise.round.observed"
	resolvedType   = "exercise.round.resolved"
	lostType       = "exercise.objective.lost"
	concludedType  = "exercise.concluded"
	assessmentType = "intelligence.assessment.issued"
	directiveType  = "command.directive.issued"
	ordersType     = "operations.orders.issued"
)

// The scenarios' own readings of the services' payloads, as far as any
// scenario reads them: the services share no Go types, and courier stands
// apart from them. Each is named for the payload it reads, and one reading
// serves every scenario that reads the payload.
type (
	point struct {
		X int `json:"x"`
		Y int `json:"y"`
	}
	location struct {
		Sector string `json:"sector"`
		point
	}
	// element is an exercise element as a faction or the observer sees it.
	// The narration calls it a squad.
	element struct {
		ID       string   `json:"id"`
		Kind     string   `json:"kind"`
		Strength int      `json:"strength"`
		Health   []int    `json:"health"`
		Status   string   `json:"status"`
		At       location `json:"at"`
	}
	// sighting is an objective in sight, with its holder.
	sighting struct {
		At     location `json:"at"`
		Holder string   `json:"holder"`
	}
	// observation is what a faction sees of a round: its own elements, the
	// enemy elements in sight, and the objectives in sight.
	observation struct {
		Own        []element  `json:"own"`
		Contacts   []element  `json:"contacts"`
		Objectives []sighting `json:"objectives"`
	}
	// observed is exercise's observation of a round for one faction.
	observed struct {
		Exercise string `json:"exercise"`
		Faction  string `json:"faction"`
		Round    int    `json:"round"`
		observation
	}
	// started is exercise's start: the map's sectors, the factions, and the
	// round's pace and limit.
	started struct {
		Exercise string `json:"exercise"`
		Name     string `json:"name"`
		Map      struct {
			Sectors []struct {
				ID     string `json:"id"`
				Width  int    `json:"width"`
				Height int    `json:"height"`
			} `json:"sectors"`
		} `json:"map"`
		Factions        []string `json:"factions"`
		RoundIntervalMS int64    `json:"round_interval_ms"`
		RoundLimit      int      `json:"round_limit"`
	}
	// engaged is an element's strength through a fight or a retreat, with
	// the operators it lost.
	engaged struct {
		ID      string `json:"id"`
		Faction string `json:"faction"`
		Before  int    `json:"before"`
		After   int    `json:"after"`
		Fallen  int    `json:"fallen"`
	}
	// retreat is an element falling back, with the enemy elements that fired
	// on it (its pursuers) and their strength through its return fire.
	retreat struct {
		engaged
		From     location  `json:"from"`
		To       location  `json:"to"`
		Pursuers []engaged `json:"pursuers"`
	}
	engagement struct {
		At       location  `json:"at"`
		Elements []engaged `json:"elements"`
	}
	// capture is an objective taken, from its former holder or "".
	capture struct {
		At      location `json:"at"`
		Faction string   `json:"faction"`
		From    string   `json:"from"`
	}
	// loss is an element destroyed.
	loss struct {
		ID      string `json:"id"`
		Faction string `json:"faction"`
	}
	// advance is a faction's progress toward taking an objective, in rounds
	// held alone.
	advance struct {
		At      location `json:"at"`
		Faction string   `json:"faction"`
		Rounds  int      `json:"rounds"`
	}
	// resolution is what a round's resolution changed.
	resolution struct {
		Retreats    []retreat    `json:"retreats"`
		Engagements []engagement `json:"engagements"`
		Captures    []capture    `json:"captures"`
		Losses      []loss       `json:"losses"`
		Progress    []advance    `json:"progress"`
	}
	// resolved is exercise's record of a round.
	resolved struct {
		Exercise string `json:"exercise"`
		Round    int    `json:"round"`
		resolution
	}
	// lost is exercise's alert to a faction that lost an objective.
	lost struct {
		Exercise string   `json:"exercise"`
		Faction  string   `json:"faction"`
		Round    int      `json:"round"`
		At       location `json:"at"`
		Holder   string   `json:"holder"`
	}
	// verdict is how an exercise ended: its winner, or "", and why.
	verdict struct {
		Winner string `json:"winner"`
		Reason string `json:"reason"`
	}
	// concluded is exercise's conclusion.
	concluded struct {
		Exercise string `json:"exercise"`
		Round    int    `json:"round"`
		verdict
	}
	// contact is an enemy element an assessment knows, seen in round Seen,
	// Age rounds ago.
	contact struct {
		element
		Seen int `json:"seen"`
		Age  int `json:"age"`
	}
	// belief is what an assessment believes of an objective: its holder,
	// seen in round Seen, Age rounds ago.
	belief struct {
		At     location `json:"at"`
		Holder string   `json:"holder"`
		Seen   int      `json:"seen"`
		Age    int      `json:"age"`
	}
	// assessment is intelligence's assessment of a round for one faction.
	assessment struct {
		Exercise   string    `json:"exercise"`
		Faction    string    `json:"faction"`
		Round      int       `json:"round"`
		Revision   int       `json:"revision"`
		Own        []element `json:"own"`
		Contacts   []contact `json:"contacts"`
		Objectives []belief  `json:"objectives"`
	}
	// directive is command's directive to one element. Its rule and contact
	// are command's; the directives scenario's stand-in sets neither.
	directive struct {
		Element string    `json:"element"`
		Rule    string    `json:"rule,omitempty"`
		Contact string    `json:"contact,omitempty"`
		Target  *location `json:"target"`
	}
	// directives is command's directives for a faction's round, which the
	// directives scenario also issues.
	directives struct {
		Exercise   string      `json:"exercise"`
		Faction    string      `json:"faction"`
		Round      int         `json:"round"`
		Directives []directive `json:"directives"`
	}
	// order is operations' order to one element.
	order struct {
		Element string     `json:"element"`
		Steps   []location `json:"steps"`
		Retreat bool       `json:"retreat"`
		Pursue  bool       `json:"pursue"`
	}
	// orders is operations' orders for a faction's round.
	orders struct {
		Exercise string  `json:"exercise"`
		Faction  string  `json:"faction"`
		Round    int     `json:"round"`
		Orders   []order `json:"orders"`
	}
)

// exerciseOf returns the exercise e's data names, or "" when the data does
// not decode. A scenario ignores an event of any exercise but its own.
func exerciseOf(e event.Event) string {
	var head struct {
		Exercise string `json:"exercise"`
	}
	if err := json.Unmarshal(e.Data, &head); err != nil {
		return ""
	}
	return head.Exercise
}

// decode reads e's data as a T. It fails permanently, since a redelivery
// holds the same data.
func decode[T any](e event.Event) (T, error) {
	var v T
	if err := json.Unmarshal(e.Data, &v); err != nil {
		return v, event.Permanent(err)
	}
	return v, nil
}
