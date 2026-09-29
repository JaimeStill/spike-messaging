package exercise_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/JaimeStill/spike-messaging/services/exercise/domain/exercise"
)

// The theater fixture, the exercise `mise run demo-theater` creates, is a
// valid exercise: its map, factions, and elements pass the create command's
// rules.
func TestTheaterFixtureIsValid(t *testing.T) {
	b, err := os.ReadFile("../../fixtures/theater.json")
	if err != nil {
		t.Fatal(err)
	}
	var c exercise.CreateExercise
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if n := len(c.Map.Sectors); n != 3 {
		t.Errorf("the theater has %d sectors, want 3", n)
	}
}
