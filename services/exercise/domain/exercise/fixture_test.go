package exercise_test

import (
	"github.com/JaimeStill/spike-messaging/services/exercise/domain/exercise"
	"github.com/JaimeStill/spike-messaging/services/exercise/domain/exercise/rules"
)

// The fixture is one 6×6 sector "a" with an objective at 3,0, a red force
// in the corner at 0,0 and a blue force in the opposite corner at 5,5: two
// factions apart, neither on the objective, neither seeing the other.
func fixture() exercise.CreateExercise {
	return exercise.CreateExercise{
		Name: "fixture",
		Map: rules.Map{Sectors: []rules.Sector{{
			ID: "a", Width: 6, Height: 6,
			Objectives: []rules.Point{{X: 3, Y: 0}},
		}}},
		Factions: [2]string{"red", "blue"},
		Elements: []rules.Element{
			{ID: "r1", Faction: "red", Kind: rules.Force, Strength: 3, At: loc(0, 0)},
			{ID: "b1", Faction: "blue", Kind: rules.Force, Strength: 3, At: loc(5, 5)},
		},
		RoundInterval: "10ms",
		RoundLimit:    3,
	}
}

func loc(x, y int) rules.Location {
	return rules.Location{Sector: "a", Point: rules.Point{X: x, Y: y}}
}
