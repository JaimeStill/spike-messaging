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
		}, []string{`element "r1": kind "tank" is neither force nor scout`}},
		{"a scout stronger than 1", func(s *rules.State) {
			s.Elements[2].Strength = 2
		}, []string{`element "r2": a scout of strength 2, want 1`}},
		{"a force of strength 0", func(s *rules.State) {
			s.Elements[0].Strength = 0
		}, []string{`element "r1": a force of strength 0, want at least 1`}},
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
		}, []string{`element "r2": at a:0,0, a cell element "r1" of its faction holds`}},
		{"elements of both factions on one cell", func(s *rules.State) {
			s.Elements[1].At = s.Elements[0].At
		}, nil},
		{"a holder of a cell that is not an objective", func(s *rules.State) {
			s.Holders["a:1,1"] = "red"
		}, []string{"holder of a:1,1: not an objective"}},
		{"a holder outside the exercise", func(s *rules.State) {
			s.Holders["a:3,0"] = "green"
		}, []string{`holder of a:3,0: faction "green" is not in the exercise`}},
		{"every failure is reported", func(s *rules.State) {
			s.Elements[0].Strength = 0
			s.Elements[1].Kind = "tank"
			s.Holders["b:5,5"] = ""
		}, []string{"a force of strength 0", `kind "tank"`, `holder of b:5,5: faction ""`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := state(
				force("r1", "red", 3, loc("a", 0, 0)),
				force("b1", "blue", 3, loc("b", 5, 4)),
				scout("r2", "red", loc("a", 1, 0)),
			)
			s.Holders["a:3,0"] = "red"
			tt.edit(&s)
			checkErr(t, s.Validate(), tt.wants)
		})
	}
}
