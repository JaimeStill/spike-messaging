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
	return rules.Location{Sector: sector, X: x, Y: y}
}

// seed is the seed every test resolves its rounds under.
const seed = 7

// squad returns a ready squad of four operators at full health.
func squad(id, faction string, at rules.Location) rules.Element {
	return rules.Element{
		ID: id, Faction: faction, Kind: rules.Squad,
		Strength: 400, Health: []int{100, 100, 100, 100}, Status: rules.StatusReady, At: at,
	}
}

// scout returns a ready scout at full health.
func scout(id, faction string, at rules.Location) rules.Element {
	return rules.Element{
		ID: id, Faction: faction, Kind: rules.Scout,
		Strength: 100, Health: []int{100}, Status: rules.StatusReady, At: at,
	}
}

// hurt returns e with its operators at health, and its strength their sum.
func hurt(e rules.Element, health ...int) rules.Element {
	e.Health, e.Strength = health, 0
	for _, h := range health {
		e.Strength += h
	}
	return e
}

// engaged returns e with its status engaged.
func engaged(e rules.Element) rules.Element {
	e.Status = rules.StatusEngaged
	return e
}

// state returns a state on the fixture map between factions "red" and
// "blue", holding no objective.
func state(es ...rules.Element) rules.State {
	return rules.State{
		Map:      fixtureMap(),
		Factions: [2]string{"red", "blue"},
		Elements: es,
		Holders:  map[string]string{},
		Progress: map[string]rules.Progress{},
	}
}

func order(id string, steps ...rules.Location) rules.Order {
	return rules.Order{Element: id, Steps: steps}
}

func pursue(id string) rules.Order {
	return rules.Order{Element: id, Pursue: true}
}

func retreat(id string, steps ...rules.Location) rules.Order {
	return rules.Order{Element: id, Steps: steps, Retreat: true}
}

// recovering returns e with its status recovering.
func recovering(e rules.Element) rules.Element {
	e.Status = rules.StatusRecovering
	return e
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
