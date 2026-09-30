package rules_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/JaimeStill/spike-messaging/services/exercise/domain/exercise/rules"
)

func TestKindStats(t *testing.T) {
	tests := []struct {
		name                    string
		kind                    rules.Kind
		moves, sight, operators int
	}{
		{"a squad of four moves once and sees one cell", rules.Squad, 1, 1, 4},
		{"a scout of one moves twice and sees two cells", rules.Scout, 2, 2, 1},
		{"an unknown kind neither moves nor sees", rules.Kind("tank"), 0, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.kind.Moves(); got != tt.moves {
				t.Errorf("Moves() = %d, want %d", got, tt.moves)
			}
			if got := tt.kind.Sight(); got != tt.sight {
				t.Errorf("Sight() = %d, want %d", got, tt.sight)
			}
			if got := tt.kind.Operators(); got != tt.operators {
				t.Errorf("Operators() = %d, want %d", got, tt.operators)
			}
		})
	}
}

func TestLocationKey(t *testing.T) {
	tests := []struct {
		name string
		at   rules.Location
		want string
	}{
		{"a cell keys as sector:x,y", loc("a", 3, 0), "a:3,0"},
		{"multi-digit coordinates stay distinct", loc("a", 1, 23), "a:1,23"},
		{"the swapped coordinates key differently", loc("a", 12, 3), "a:12,3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.at.Key(); got != tt.want {
				t.Errorf("Key() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLocationJSONFlattensThePoint(t *testing.T) {
	b, err := json.Marshal(loc("a", 1, 2))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(b), `{"sector":"a","x":1,"y":2}`; got != want {
		t.Errorf("json = %s, want %s", got, want)
	}
}

func TestMapValidate(t *testing.T) {
	tests := []struct {
		name  string
		edit  func(m *rules.Map)
		wants []string
	}{
		{"the fixture map is valid", func(*rules.Map) {}, nil},
		{"a sector with an empty ID", func(m *rules.Map) {
			m.Sectors = append(m.Sectors, rules.Sector{Width: 1, Height: 1})
		}, []string{"sector 2: an empty ID"}},
		{"two sectors with one ID", func(m *rules.Map) {
			m.Sectors = append(m.Sectors, rules.Sector{ID: "a", Width: 1, Height: 1})
		}, []string{`sector "a": a duplicate ID`}},
		{"a grid narrower than one cell", func(m *rules.Map) {
			m.Sectors = append(m.Sectors, rules.Sector{ID: "c", Width: 0, Height: 3})
		}, []string{`sector "c": a 0×3 grid`}},
		{"an obstacle outside the grid", func(m *rules.Map) {
			m.Sectors[0].Obstacles = append(m.Sectors[0].Obstacles, rules.Point{X: 6, Y: 0})
		}, []string{"an obstacle at 6,0 lies outside the grid"}},
		{"an objective outside the grid", func(m *rules.Map) {
			m.Sectors[0].Objectives = append(m.Sectors[0].Objectives, rules.Point{X: 0, Y: -1})
		}, []string{"an objective at 0,-1 lies outside the grid"}},
		{"an objective on an obstacle", func(m *rules.Map) {
			m.Sectors[0].Objectives = append(m.Sectors[0].Objectives, rules.Point{X: 2, Y: 2})
		}, []string{"an objective at 2,2 shares its cell with an obstacle"}},
		{"two objectives on one cell", func(m *rules.Map) {
			m.Sectors[0].Objectives = append(m.Sectors[0].Objectives, rules.Point{X: 3, Y: 0})
		}, []string{"an objective at 3,0 shares its cell with an objective"}},
		{"a gate linking to its own sector", func(m *rules.Map) {
			m.Sectors[0].Gates = append(m.Sectors[0].Gates, rules.Gate{At: rules.Point{X: 0, Y: 5}, To: loc("a", 1, 5)})
		}, []string{"the gate at 0,5 links to its own sector"}},
		{"a gate linking to a missing sector", func(m *rules.Map) {
			m.Sectors[0].Gates[0].To.Sector = "z"
		}, []string{`the gate at 5,5 links to sector "z", which does not exist`}},
		{"a gate linking outside the other grid", func(m *rules.Map) {
			m.Sectors[0].Gates[0].To = loc("b", 9, 9)
		}, []string{"the gate at 5,5 links to b:9,9, outside its grid"}},
		{"a gate linking to a cell that is not a gate", func(m *rules.Map) {
			m.Sectors[0].Gates[0].To = loc("b", 1, 1)
		}, []string{"the gate at 5,5 links to b:1,1, which is not a gate"}},
		{"a gate whose link does not point back", func(m *rules.Map) {
			m.Sectors[1].Gates[0].To = loc("a", 4, 5)
		}, []string{"the gate at 5,5 links to b:0,0, which links to a:4,5 instead"}},
		{"a map without objectives", func(m *rules.Map) {
			m.Sectors[0].Objectives = nil
			m.Sectors[1].Objectives = nil
		}, []string{"no objective"}},
		{"every failure is reported", func(m *rules.Map) {
			m.Sectors[0].Obstacles = append(m.Sectors[0].Obstacles, rules.Point{X: 7, Y: 7})
			m.Sectors[1].ID = ""
			m.Sectors[0].Objectives = nil
			m.Sectors[1].Objectives = nil
		}, []string{"an obstacle at 7,7 lies outside the grid", "sector 1: an empty ID", "no objective", `links to sector "b"`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := fixtureMap()
			tt.edit(&m)
			checkErr(t, m.Validate(), tt.wants)
		})
	}
}

// checkErr fails t unless err is nil when wants is empty, or otherwise is a
// rules error naming every one of wants.
func checkErr(t *testing.T, err error, wants []string) {
	t.Helper()
	if len(wants) == 0 {
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		return
	}
	if err == nil {
		t.Fatalf("err = nil, want %q", wants)
	}
	if !strings.HasPrefix(err.Error(), "rules: ") {
		t.Errorf("err = %q, want the prefix %q", err, "rules: ")
	}
	for _, w := range wants {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("err = %q, want it to contain %q", err, w)
		}
	}
}

// The terrain is the map without its objectives, and shares nothing with it.
func TestTerrainHidesTheObjectives(t *testing.T) {
	m := fixtureMap()
	terrain := m.Terrain()
	for i, sec := range terrain.Sectors {
		if len(sec.Objectives) != 0 {
			t.Errorf("sector %s: objectives %v, want none", sec.ID, sec.Objectives)
		}
		if sec.ID != m.Sectors[i].ID || len(sec.Obstacles) != len(m.Sectors[i].Obstacles) || len(sec.Gates) != len(m.Sectors[i].Gates) {
			t.Errorf("sector %s: %+v, want the rest of the map", sec.ID, sec)
		}
	}
	if len(m.Sectors[0].Objectives) != 1 {
		t.Errorf("Terrain changed the map: %+v", m.Sectors[0])
	}
}

// Every kind the package lists moves, sees, and fields operators, so a kind
// added to Kinds without its rules fails here rather than reach a client
// with a sight of 0.
func TestEveryKindHasItsRules(t *testing.T) {
	for _, k := range rules.Kinds {
		if k.Moves() < 1 || k.Sight() < 1 || k.Operators() < 1 {
			t.Errorf("%s: moves %d, sight %d, operators %d, want each positive", k, k.Moves(), k.Sight(), k.Operators())
		}
	}
}
