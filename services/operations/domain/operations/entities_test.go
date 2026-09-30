package operations_test

import (
	"errors"
	"testing"

	"github.com/JaimeStill/spike-messaging/services/operations/domain/operations"
)

const exerciseID = "01999f4e-6a3b-7c2d-8e1f-0a1b2c3d4e5f"

func TestValidate(t *testing.T) {
	valid := []interface{ Validate() error }{
		operations.Open{Exercise: exerciseID, Factions: [2]string{"red", "blue"}, RoundLimit: 1},
		operations.Assign{Exercise: exerciseID, Faction: "red", Sequence: 1, Directives: []operations.Directive{{Element: "r1"}}},
		operations.Maneuver{Exercise: exerciseID, Faction: "red"},
		operations.Close{Exercise: exerciseID},
	}
	for _, c := range valid {
		if err := c.Validate(); err != nil {
			t.Errorf("%T: %v", c, err)
		}
	}
	invalid := map[string]interface{ Validate() error }{
		"open, no uuid":            operations.Open{Exercise: "x", Factions: [2]string{"red", "blue"}, RoundLimit: 1},
		"open, same factions":      operations.Open{Exercise: exerciseID, Factions: [2]string{"red", "red"}, RoundLimit: 1},
		"open, empty faction":      operations.Open{Exercise: exerciseID, Factions: [2]string{"red", ""}, RoundLimit: 1},
		"open, no round limit":     operations.Open{Exercise: exerciseID, Factions: [2]string{"red", "blue"}},
		"assign, no faction":       operations.Assign{Exercise: exerciseID},
		"assign, no sequence":      operations.Assign{Exercise: exerciseID, Faction: "red"},
		"assign, no element":       operations.Assign{Exercise: exerciseID, Faction: "red", Sequence: 1, Directives: []operations.Directive{{}}},
		"maneuver, negative round": operations.Maneuver{Exercise: exerciseID, Faction: "red", Round: -1},
		"close, no uuid":           operations.Close{Exercise: "x"},
	}
	for name, c := range invalid {
		if err := c.Validate(); !errors.Is(err, operations.ErrValidation) {
			t.Errorf("%s: Validate = %v, want ErrValidation", name, err)
		}
	}
}
