package rules_test

import (
	"reflect"
	"testing"

	"github.com/JaimeStill/spike-messaging/services/exercise/domain/exercise/rules"
)

var factions = [2]string{"red", "blue"}

// Every seed lays out a valid skirmish: one 13×13 sector with no obstacles
// or gates, three to five objectives on rows 3 to 9, and each faction's
// three squads and two scouts on its baseline, every element mirrored
// through the center by one of the other faction's.
func TestSkirmishIsValidAndMirrored(t *testing.T) {
	const last = rules.SkirmishSize - 1
	for sd := range int64(200) {
		s := rules.Skirmish(sd, factions)
		if err := s.Validate(); err != nil {
			t.Fatalf("seed %d: %v", sd, err)
		}
		if len(s.Map.Sectors) != 1 {
			t.Fatalf("seed %d: %d sectors, want 1", sd, len(s.Map.Sectors))
		}
		sec := s.Map.Sectors[0]
		if sec.Width != rules.SkirmishSize || sec.Height != rules.SkirmishSize || len(sec.Obstacles) != 0 || len(sec.Gates) != 0 {
			t.Fatalf("seed %d: sector %+v", sd, sec)
		}
		objectives := map[rules.Point]bool{}
		for _, p := range sec.Objectives {
			objectives[p] = true
			if p.Y < 3 || p.Y > 9 {
				t.Errorf("seed %d: an objective at %d,%d, off rows 3 to 9", sd, p.X, p.Y)
			}
		}
		if n := len(objectives); n < 3 || n > 5 || n != len(sec.Objectives) {
			t.Errorf("seed %d: %d objectives, want 3 to 5, distinct", sd, len(sec.Objectives))
		}
		for p := range objectives {
			if !objectives[rules.Point{X: last - p.X, Y: last - p.Y}] {
				t.Errorf("seed %d: the objective at %d,%d is not mirrored", sd, p.X, p.Y)
			}
		}
		kinds := map[string]map[rules.Kind]int{}
		at := map[string]rules.Element{}
		for _, e := range s.Elements {
			if kinds[e.Faction] == nil {
				kinds[e.Faction] = map[rules.Kind]int{}
			}
			kinds[e.Faction][e.Kind]++
			at[e.Faction+"@"+e.At.Key()] = e
			if e.Strength != 100*e.Kind.Operators() || e.Status != rules.StatusReady {
				t.Errorf("seed %d: %+v, want full health and ready", sd, e)
			}
			if home := map[string]int{"red": 0, "blue": last}[e.Faction]; e.At.Y != home {
				t.Errorf("seed %d: %s on row %d, want its baseline %d", sd, e.ID, e.At.Y, home)
			}
		}
		for _, f := range factions {
			if want := map[rules.Kind]int{rules.Squad: 3, rules.Scout: 2}; !reflect.DeepEqual(kinds[f], want) {
				t.Errorf("seed %d: %s fields %v, want %v", sd, f, kinds[f], want)
			}
		}
		for _, e := range s.Elements {
			if e.Faction != "red" {
				continue
			}
			m := e.At
			m.X, m.Y = last-m.X, last-m.Y
			if b, ok := at["blue@"+m.Key()]; !ok || b.Kind != e.Kind {
				t.Errorf("seed %d: %s at %s has no mirrored %s", sd, e.ID, e.At.Key(), e.Kind)
			}
		}
	}
}

// One seed always lays out the same skirmish, and seeds differ.
func TestSkirmishIsDrawnFromItsSeed(t *testing.T) {
	if a, b := rules.Skirmish(42, factions), rules.Skirmish(42, factions); !reflect.DeepEqual(a, b) {
		t.Errorf("seed 42 laid out two skirmishes")
	}
	layouts := map[string]bool{}
	for sd := range int64(20) {
		s := rules.Skirmish(sd, factions)
		key := ""
		for _, p := range s.Map.Sectors[0].Objectives {
			key += rules.Location{Point: p}.Key() + " "
		}
		for _, e := range s.Elements {
			key += e.At.Key() + " "
		}
		layouts[key] = true
	}
	if len(layouts) < 15 {
		t.Errorf("twenty seeds laid out %d skirmishes, want nearly all distinct", len(layouts))
	}
}

// Element IDs take each faction's initial, squads first, or its whole name
// when both factions share an initial.
func TestSkirmishNamesItsElements(t *testing.T) {
	tests := []struct {
		factions [2]string
		want     []string
	}{
		{[2]string{"red", "blue"}, []string{"b1", "b2", "b3", "b4", "b5", "r1", "r2", "r3", "r4", "r5"}},
		{[2]string{"blue", "black"}, []string{"black1", "black2", "black3", "black4", "black5", "blue1", "blue2", "blue3", "blue4", "blue5"}},
	}
	for _, tt := range tests {
		s := rules.Skirmish(1, tt.factions)
		if got := ids(s.Elements); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%v: IDs %v, want %v", tt.factions, got, tt.want)
		}
		for _, e := range s.Elements {
			if n := e.ID[len(e.ID)-1]; (n <= '3') != (e.Kind == rules.Squad) {
				t.Errorf("%v: %s is a %s", tt.factions, e.ID, e.Kind)
			}
		}
	}
}
