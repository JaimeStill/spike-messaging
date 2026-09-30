package exercise_test

import (
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/JaimeStill/spike-messaging/services/exercise/domain/exercise"
	"github.com/JaimeStill/spike-messaging/services/exercise/domain/exercise/rules"
)

// The theater fixture, the large exercise that `FIXTURE=theater mise run
// demo-theater` creates, is a valid exercise: its map, factions, and
// elements pass the create command's rules.
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

// The skirmish fixture, the exercise `mise run demo-theater` creates by
// default, is a valid exercise that favors neither side. A point reflection
// of each sector maps its obstacles, objectives, and gates onto themselves,
// and each red element onto a blue one of the same kind and strength.
func TestSkirmishFixtureIsValidAndSymmetric(t *testing.T) {
	b, err := os.ReadFile("../../fixtures/skirmish.json")
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
	if n := len(c.Map.Sectors); n != 2 || c.RoundLimit != 20 || c.RoundInterval != "1s" {
		t.Errorf("the skirmish has %d sectors, %d rounds of %s; want 2, 20 of 1s", n, c.RoundLimit, c.RoundInterval)
	}
	size := map[string]rules.Point{}
	for _, s := range c.Map.Sectors {
		size[s.ID] = rules.Point{X: s.Width, Y: s.Height}
	}
	mirror := func(l rules.Location) rules.Location {
		wh := size[l.Sector]
		return rules.Location{Sector: l.Sector, Point: rules.Point{X: wh.X - 1 - l.X, Y: wh.Y - 1 - l.Y}}
	}
	point := func(l rules.Location) rules.Point { return l.Point }
	for _, s := range c.Map.Sectors {
		at := func(p rules.Point) rules.Location { return rules.Location{Sector: s.ID, Point: p} }
		for _, set := range [][]rules.Point{s.Obstacles, s.Objectives} {
			for _, p := range set {
				if m := point(mirror(at(p))); !slices.Contains(set, m) {
					t.Errorf("%s:%d,%d has no mirror at %d,%d", s.ID, p.X, p.Y, m.X, m.Y)
				}
			}
		}
		for _, g := range s.Gates {
			m := rules.Gate{At: point(mirror(at(g.At))), To: mirror(g.To)}
			if !slices.Contains(s.Gates, m) {
				t.Errorf("gate %+v has no mirror %+v", g, m)
			}
		}
	}
	reds := 0
	for _, r := range c.Elements {
		if r.Faction != "red" {
			continue
		}
		reds++
		m := mirror(r.At)
		if !slices.ContainsFunc(c.Elements, func(b rules.Element) bool {
			return b.Faction == "blue" && b.Kind == r.Kind && b.Strength == r.Strength && b.At == m
		}) {
			t.Errorf("%s has no blue mirror at %s", r.ID, m.Key())
		}
	}
	if reds != 3 || len(c.Elements) != 6 {
		t.Errorf("the skirmish has %d elements, %d red; want 3 a side", len(c.Elements), reds)
	}
}
