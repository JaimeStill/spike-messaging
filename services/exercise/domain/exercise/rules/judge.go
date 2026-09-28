package rules

// The reasons a [Verdict] gives for an exercise that is over.
const (
	reasonElimination = "elimination"
	reasonObjectives  = "objectives"
	reasonLimit       = "limit"
)

// Verdict is the judgment of an exercise after a round. When Over, Winner
// is the winning faction, or "" for a draw, and Reason is "elimination",
// "objectives", or "limit". An exercise that is not over has neither.
type Verdict struct {
	Over   bool   `json:"over"`
	Winner string `json:"winner"`
	Reason string `json:"reason"`
}

// Judge judges s after round against limit, the exercise's round limit. A
// faction with no elements loses, and when neither faction has any the
// exercise is a draw by elimination. Otherwise a faction holding every
// objective on the map wins by objectives. Otherwise, once round reaches
// limit, the faction holding more objectives wins, and equal holdings are a
// draw. Before the limit the exercise is not over. [Resolve] judges each
// round it resolves; Judge alone judges a state no round has changed, such
// as the start.
func Judge(s State, round, limit int) Verdict {
	var alive, held [2]int
	for _, e := range s.Elements {
		if i, ok := s.index(e.Faction); ok {
			alive[i]++
		}
	}
	for _, o := range s.Map.objectives() {
		if i, ok := s.index(s.Holders[o.Key()]); ok {
			held[i]++
		}
	}
	total := len(s.Map.objectives())
	switch {
	case alive[0] == 0 && alive[1] == 0:
		return Verdict{Over: true, Reason: reasonElimination}
	case alive[0] == 0:
		return Verdict{Over: true, Winner: s.Factions[1], Reason: reasonElimination}
	case alive[1] == 0:
		return Verdict{Over: true, Winner: s.Factions[0], Reason: reasonElimination}
	case total > 0 && held[0] == total:
		return Verdict{Over: true, Winner: s.Factions[0], Reason: reasonObjectives}
	case total > 0 && held[1] == total:
		return Verdict{Over: true, Winner: s.Factions[1], Reason: reasonObjectives}
	case round < limit:
		return Verdict{}
	case held[0] > held[1]:
		return Verdict{Over: true, Winner: s.Factions[0], Reason: reasonLimit}
	case held[1] > held[0]:
		return Verdict{Over: true, Winner: s.Factions[1], Reason: reasonLimit}
	}
	return Verdict{Over: true, Reason: reasonLimit}
}

// index returns the position of faction f in s.Factions.
func (s State) index(f string) (int, bool) {
	if f == "" {
		return 0, false
	}
	switch f {
	case s.Factions[0]:
		return 0, true
	case s.Factions[1]:
		return 1, true
	}
	return 0, false
}
