package rules_test

import (
	"testing"

	"github.com/JaimeStill/spike-messaging/services/exercise/domain/exercise/rules"
)

func TestStateValidate(t *testing.T) {
	tests := []struct {
		name  string
		edit  func(s *rules.State)
		wants []string
	}{
		{"a valid state", func(*rules.State) {}, nil},
		{"an invalid map", func(s *rules.State) {
			s.Map.Sectors[0].Objectives = nil
			s.Map.Sectors[1].Objectives = nil
			s.Holders = nil
		}, []string{"rules: state: rules: map: no objective"}},
		{"an unnamed faction", func(s *rules.State) {
			s.Factions[1] = ""
			s.Elements = s.Elements[:1]
		}, []string{"a faction is unnamed"}},
		{"the same faction twice", func(s *rules.State) {
			s.Factions[1] = "red"
			s.Elements = s.Elements[:1]
		}, []string{"the factions are the same"}},
		{"an element with an empty ID", func(s *rules.State) {
			s.Elements[0].ID = ""
		}, []string{"element 0: an empty ID"}},
		{"two elements with one ID", func(s *rules.State) {
			s.Elements[1].ID = "r1"
		}, []string{`element "r1": a duplicate ID`}},
		{"an element of a faction outside the exercise", func(s *rules.State) {
			s.Elements[0].Faction = "green"
		}, []string{`element "r1": faction "green" is not in the exercise`}},
		{"an element of an unknown kind", func(s *rules.State) {
			s.Elements[0].Kind = "tank"
		}, []string{`element "r1": kind "tank" is neither squad nor scout`}},
		{"a scout of two operators", func(s *rules.State) {
			s.Elements[2] = hurt(s.Elements[2], 100, 100)
		}, []string{`element "r2": 2 operators, want 1 to 1`}},
		{"a squad of five operators", func(s *rules.State) {
			s.Elements[0] = hurt(s.Elements[0], 100, 100, 100, 100, 100)
		}, []string{`element "r1": 5 operators, want 1 to 4`}},
		{"a squad of no operators", func(s *rules.State) {
			s.Elements[0] = hurt(s.Elements[0])
		}, []string{`element "r1": 0 operators, want 1 to 4`}},
		{"a wounded squad", func(s *rules.State) {
			s.Elements[0] = hurt(s.Elements[0], 100, 12, 1)
		}, nil},
		{"an operator at health 0", func(s *rules.State) {
			s.Elements[0] = hurt(s.Elements[0], 100, 0)
		}, []string{`element "r1": an operator at health 0, want 1 to 100`}},
		{"an operator above 100", func(s *rules.State) {
			s.Elements[0] = hurt(s.Elements[0], 101)
		}, []string{`element "r1": an operator at health 101, want 1 to 100`}},
		{"a strength that is not the health's sum", func(s *rules.State) {
			s.Elements[0].Strength = 300
		}, []string{`element "r1": strength 300, want 400, the sum of its health`}},
		{"an unknown status", func(s *rules.State) {
			s.Elements[0].Status = "asleep"
		}, []string{`element "r1": status "asleep" is not ready, engaged, or recovering`}},
		{"an element in a missing sector", func(s *rules.State) {
			s.Elements[0].At = loc("z", 0, 0)
		}, []string{`element "r1": at z:0,0, in a sector that does not exist`}},
		{"an element outside the grid", func(s *rules.State) {
			s.Elements[0].At = loc("a", 6, 0)
		}, []string{`element "r1": at a:6,0, outside the grid`}},
		{"an element on an obstacle", func(s *rules.State) {
			s.Elements[0].At = loc("a", 2, 2)
		}, []string{`element "r1": at a:2,2, on an obstacle`}},
		{"two elements of one faction on one cell", func(s *rules.State) {
			s.Elements[2].At = s.Elements[0].At
		}, nil},
		{"elements of both factions on one cell", func(s *rules.State) {
			s.Elements[1].At = s.Elements[0].At
		}, nil},
		{"a holder of a cell that is not an objective", func(s *rules.State) {
			s.Holders["a:1,1"] = "red"
		}, []string{"holder of a:1,1: not an objective"}},
		{"a holder outside the exercise", func(s *rules.State) {
			s.Holders["a:3,0"] = "green"
		}, []string{`holder of a:3,0: faction "green" is not in the exercise`}},
		{"progress on a cell that is not an objective", func(s *rules.State) {
			s.Progress["a:1,1"] = rules.Progress{Faction: "red", Rounds: 1}
		}, []string{"progress on a:1,1: not an objective"}},
		{"progress by a faction outside the exercise", func(s *rules.State) {
			s.Progress["b:5,5"] = rules.Progress{Faction: "green", Rounds: 1}
		}, []string{`progress on b:5,5: faction "green" is not in the exercise`}},
		{"progress that should have captured", func(s *rules.State) {
			s.Progress["b:5,5"] = rules.Progress{Faction: "blue", Rounds: 2}
		}, []string{"progress on b:5,5: 2 rounds, want 1 to 1"}},
		{"every failure is reported", func(s *rules.State) {
			s.Elements[0].Strength = 0
			s.Elements[1].Kind = "tank"
			s.Holders["b:5,5"] = ""
		}, []string{"strength 0, want 400", `kind "tank"`, `holder of b:5,5: faction ""`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := state(
				squad("r1", "red", loc("a", 0, 0)),
				squad("b1", "blue", loc("b", 5, 4)),
				scout("r2", "red", loc("a", 1, 0)),
			)
			s.Holders["a:3,0"] = "red"
			tt.edit(&s)
			checkErr(t, s.Validate(), tt.wants)
		})
	}
}
