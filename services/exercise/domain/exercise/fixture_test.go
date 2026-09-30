package exercise_test

import (
	"github.com/JaimeStill/spike-messaging/services/exercise/domain/exercise"
	"github.com/JaimeStill/spike-messaging/services/exercise/domain/exercise/rules"
)

// The fixture is one 6×6 sector "a" with an objective at 3,0, a red squad
// in the corner at 0,0 and a blue squad in the opposite corner at 5,5, each
// of three operators at full health: two factions apart, neither on the
// objective, neither seeing the other.
func fixture() exercise.CreateExercise {
	return exercise.CreateExercise{
		Name: "fixture",
		Map: &rules.Map{Sectors: []rules.Sector{{
			ID: "a", Width: 6, Height: 6,
			Objectives: []rules.Point{{X: 3, Y: 0}},
		}}},
		Factions:      [2]string{"red", "blue"},
		Elements:      []rules.Element{squad("r1", "red", loc(0, 0)), squad("b1", "blue", loc(5, 5))},
		RoundInterval: "10ms",
		RoundLimit:    3,
	}
}

// squad returns a ready squad of three operators at full health.
func squad(id, faction string, at rules.Location) rules.Element {
	return rules.Element{
		ID: id, Faction: faction, Kind: rules.Squad,
		Strength: 300, Health: []int{100, 100, 100}, Status: rules.StatusReady, At: at,
	}
}

func loc(x, y int) rules.Location {
	return rules.Location{Sector: "a", X: x, Y: y}
}
