package command_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/JaimeStill/spike-messaging/services/command/domain/command"
	"github.com/JaimeStill/spike-messaging/services/command/domain/command/decide"
)

const exerciseID = "01999f4e-6a3b-7c2d-8e1f-0a1b2c3d4e5f"

func TestValidate(t *testing.T) {
	valid := []interface{ Validate() error }{
		command.Open{Exercise: exerciseID, Factions: [2]string{"red", "blue"}},
		command.Decide{Exercise: exerciseID, Faction: "red"},
		command.Close{Exercise: exerciseID},
	}
	for _, c := range valid {
		if err := c.Validate(); err != nil {
			t.Errorf("%T: %v", c, err)
		}
	}
	invalid := map[string]interface{ Validate() error }{
		"open, no uuid":          command.Open{Exercise: "x", Factions: [2]string{"red", "blue"}},
		"open, same factions":    command.Open{Exercise: exerciseID, Factions: [2]string{"red", "red"}},
		"open, empty faction":    command.Open{Exercise: exerciseID, Factions: [2]string{"red", ""}},
		"decide, no faction":     command.Decide{Exercise: exerciseID},
		"decide, negative round": command.Decide{Exercise: exerciseID, Faction: "red", Assessment: decide.Assessment{Round: -1}},
		"close, no uuid":         command.Close{Exercise: "x"},
	}
	for name, c := range invalid {
		if err := c.Validate(); !errors.Is(err, command.ErrValidation) {
			t.Errorf("%s: Validate = %v, want ErrValidation", name, err)
		}
	}
}

// Decide decodes intelligence's assessment event whole, the assessment
// flattened beside the exercise and the faction.
func TestDecideDecodesTheAssessmentEvent(t *testing.T) {
	var c command.Decide
	err := json.Unmarshal([]byte(`{"exercise":"`+exerciseID+`","faction":"red","round":2,
		"own":[{"id":"r1","faction":"red","kind":"force","strength":2,"at":{"sector":"a","x":0,"y":0}}],
		"contacts":[{"id":"b1","faction":"blue","kind":"scout","strength":1,"at":{"sector":"a","x":1,"y":0},"seen":1,"age":1}],
		"objectives":[{"at":{"sector":"a","x":1,"y":0},"holder":"","known":false,"seen":-1,"age":0}]}`), &c)
	if err != nil {
		t.Fatal(err)
	}
	if c.Exercise != exerciseID || c.Faction != "red" || c.Round != 2 || len(c.Own) != 1 || len(c.Contacts) != 1 || len(c.Objectives) != 1 {
		t.Errorf("Decide = %+v", c)
	}
}

// The directive's payload is the shape operations reads, each directive
// with its rule, and an engage with its contact.
func TestDirectiveDataShape(t *testing.T) {
	at := decide.Location{Sector: "a", Point: decide.Point{X: 1, Y: 0}}
	d := command.DirectiveData{Exercise: exerciseID, Faction: "red", Round: 3, Directives: []decide.Decision{
		{Element: "r1", Rule: decide.Engage, Contact: "b1", Target: &at},
		{Element: "r2", Rule: decide.Secure, Target: &at},
		{Element: "r3", Rule: decide.Hold},
	}}
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"exercise":"` + exerciseID + `","faction":"red","round":3,"directives":[` +
		`{"element":"r1","rule":"engage","contact":"b1","target":{"sector":"a","x":1,"y":0}},` +
		`{"element":"r2","rule":"secure","target":{"sector":"a","x":1,"y":0}},` +
		`{"element":"r3","rule":"hold","target":null}]}`
	if string(b) != want {
		t.Errorf("payload = %s, want %s", b, want)
	}
}
