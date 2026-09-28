package rules_test

import (
	"github.com/JaimeStill/spike-messaging/services/exercise/domain/exercise/rules"
)

// The fixture map has two 6×6 sectors, "a" and "b". Sector a has an
// obstacle at 2,2, an objective at 3,0, and a gate at 5,5 linked to b's gate
// at 0,0. Sector b has an objective at 5,5.
func fixtureMap() rules.Map {
	return rules.Map{Sectors: []rules.Sector{
		{
			ID: "a", Width: 6, Height: 6,
			Obstacles:  []rules.Point{{X: 2, Y: 2}},
			Objectives: []rules.Point{{X: 3, Y: 0}},
			Gates:      []rules.Gate{{At: rules.Point{X: 5, Y: 5}, To: loc("b", 0, 0)}},
		},
		{
			ID: "b", Width: 6, Height: 6,
			Objectives: []rules.Point{{X: 5, Y: 5}},
			Gates:      []rules.Gate{{At: rules.Point{X: 0, Y: 0}, To: loc("a", 5, 5)}},
		},
	}}
}

func loc(sector string, x, y int) rules.Location {
	return rules.Location{Sector: sector, Point: rules.Point{X: x, Y: y}}
}

func force(id, faction string, strength int, at rules.Location) rules.Element {
	return rules.Element{ID: id, Faction: faction, Kind: rules.Force, Strength: strength, At: at}
}

func scout(id, faction string, at rules.Location) rules.Element {
	return rules.Element{ID: id, Faction: faction, Kind: rules.Scout, Strength: 1, At: at}
}

// state returns a state on the fixture map between factions "red" and
// "blue", holding no objective.
func state(es ...rules.Element) rules.State {
	return rules.State{
		Map:      fixtureMap(),
		Factions: [2]string{"red", "blue"},
		Elements: es,
		Holders:  map[string]string{},
	}
}

func order(id string, steps ...rules.Location) rules.Order {
	return rules.Order{Element: id, Steps: steps}
}

// find returns the element of s with ID id.
func find(s rules.State, id string) (rules.Element, bool) {
	for _, e := range s.Elements {
		if e.ID == id {
			return e, true
		}
	}
	return rules.Element{}, false
}

func ids(es []rules.Element) []string {
	out := []string{}
	for _, e := range es {
		out = append(out, e.ID)
	}
	return out
}
