package intelligence_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/JaimeStill/spike-messaging/services/intelligence/domain/intelligence"
	"github.com/JaimeStill/spike-messaging/services/intelligence/domain/intelligence/fusion"
)

const exerciseID = "01999f4e-6a3b-7c2d-8e1f-0a1b2c3d4e5f"

func TestValidate(t *testing.T) {
	valid := []interface{ Validate() error }{
		intelligence.Open{Exercise: exerciseID, Factions: [2]string{"red", "blue"}},
		intelligence.Observe{Exercise: exerciseID, Faction: "red"},
		intelligence.Close{Exercise: exerciseID},
	}
	for _, c := range valid {
		if err := c.Validate(); err != nil {
			t.Errorf("%T: %v", c, err)
		}
	}
	invalid := map[string]interface{ Validate() error }{
		"open, no uuid":           intelligence.Open{Exercise: "x", Factions: [2]string{"red", "blue"}},
		"open, same factions":     intelligence.Open{Exercise: exerciseID, Factions: [2]string{"red", "red"}},
		"open, empty faction":     intelligence.Open{Exercise: exerciseID, Factions: [2]string{"red", ""}},
		"observe, no faction":     intelligence.Observe{Exercise: exerciseID},
		"observe, negative round": intelligence.Observe{Exercise: exerciseID, Faction: "red", Observation: fusion.Observation{Round: -1}},
		"close, no uuid":          intelligence.Close{Exercise: "x"},
	}
	for name, c := range invalid {
		if err := c.Validate(); !errors.Is(err, intelligence.ErrValidation) {
			t.Errorf("%s: Validate = %v, want ErrValidation", name, err)
		}
	}
}

// Observe decodes exercise's observed event whole, the observation
// flattened beside the exercise and the faction.
func TestObserveDecodesTheObservedEvent(t *testing.T) {
	var c intelligence.Observe
	err := json.Unmarshal([]byte(`{"exercise":"`+exerciseID+`","faction":"red","round":2,
		"own":[{"id":"r1","faction":"red","kind":"squad","strength":2,"health":[100,100],"status":"ready","at":{"sector":"a","x":0,"y":0}}],
		"contacts":[],"objectives":[{"at":{"sector":"a","x":1,"y":0},"holder":""}]}`), &c)
	if err != nil {
		t.Fatal(err)
	}
	if c.Exercise != exerciseID || c.Faction != "red" || c.Round != 2 || len(c.Own) != 1 || len(c.Objectives) != 1 {
		t.Errorf("Observe = %+v", c)
	}
}

// The assessment's payload flattens the picture beside the exercise and
// the faction.
func TestAssessmentDataShape(t *testing.T) {
	d := intelligence.AssessmentData{Exercise: exerciseID, Faction: "red", Picture: fusion.Open(nil)}
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"exercise":"` + exerciseID + `","faction":"red","round":-1,"own":[],"contacts":[],"objectives":[]}`
	if string(b) != want {
		t.Errorf("payload = %s, want %s", b, want)
	}
}
