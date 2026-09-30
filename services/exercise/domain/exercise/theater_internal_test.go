package exercise

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/JaimeStill/spike-messaging/services/exercise/domain/exercise/rules"
)

// The skirmish fixture, the exercise `mise run demo-theater` creates, is a
// valid exercise that gives no map, elements, or seed, so it starts from
// the skirmish its seed lays out.
func TestSkirmishFixture(t *testing.T) {
	b, err := os.ReadFile("../../fixtures/skirmish.json")
	if err != nil {
		t.Fatal(err)
	}
	var c CreateExercise
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if c.Seed != nil || c.Map != nil || c.Elements != nil {
		t.Errorf("the fixture gives a seed, map, or elements: %+v", c)
	}
	if c.RoundLimit != 40 || c.RoundInterval != "1s" {
		t.Errorf("the fixture runs %d rounds of %s, want 40 of 1s", c.RoundLimit, c.RoundInterval)
	}
	for _, seed := range []int64{0, 7} {
		if got, want := c.state(seed), rules.Skirmish(seed, c.Factions); !reflect.DeepEqual(got, want) {
			t.Errorf("state(%d) = %+v, want the skirmish %+v", seed, got, want)
		}
	}
}
