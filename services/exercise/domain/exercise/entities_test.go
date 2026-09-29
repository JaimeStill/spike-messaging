package exercise_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/JaimeStill/spike-messaging/services/exercise/domain/exercise"
	"github.com/JaimeStill/spike-messaging/services/exercise/domain/exercise/rules"
)

func TestCreateExerciseValidate(t *testing.T) {
	cases := []struct {
		name   string
		change func(*exercise.CreateExercise)
		want   string // a fragment of the error, or "" for a valid command
	}{
		{"valid", func(*exercise.CreateExercise) {}, ""},
		{"a minute's interval", func(c *exercise.CreateExercise) { c.RoundInterval = "1m" }, ""},
		{"empty name", func(c *exercise.CreateExercise) { c.Name = " " }, "name: empty"},
		{"unparsed interval", func(c *exercise.CreateExercise) { c.RoundInterval = "soon" }, "round_interval"},
		{"empty interval", func(c *exercise.CreateExercise) { c.RoundInterval = "" }, "round_interval"},
		{"interval below the minimum", func(c *exercise.CreateExercise) { c.RoundInterval = "9ms" }, "want at least 10ms"},
		{"zero limit", func(c *exercise.CreateExercise) { c.RoundLimit = 0 }, "round_limit"},
		{"one faction twice", func(c *exercise.CreateExercise) { c.Factions = [2]string{"red", "red"} }, "the factions are the same"},
		{"no objective", func(c *exercise.CreateExercise) { c.Map.Sectors[0].Objectives = nil }, "no objective"},
		{"element off the grid", func(c *exercise.CreateExercise) { c.Elements[0].At = loc(9, 9) }, "outside the grid"},
		{"element of no faction", func(c *exercise.CreateExercise) { c.Elements[1].Faction = "green" }, `faction "green"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := fixture()
			tc.change(&c)
			err := c.Validate()
			if tc.want == "" {
				if err != nil {
					t.Fatalf("Validate = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, exercise.ErrValidation) {
				t.Fatalf("Validate = %v, want ErrValidation", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

// Every failure is reported at once, not just the first.
func TestCreateExerciseValidateJoinsFailures(t *testing.T) {
	c := fixture()
	c.Name = ""
	c.RoundLimit = 0
	c.Elements = append(c.Elements, rules.Element{ID: "r1", Faction: "red", Kind: rules.Scout, Strength: 1, At: loc(1, 1)})
	err := c.Validate()
	for _, want := range []string{"name: empty", "round_limit", "a duplicate ID"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Validate = %v, want it to mention %q", err, want)
		}
	}
}
