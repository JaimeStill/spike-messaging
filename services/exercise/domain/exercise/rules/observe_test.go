package rules_test

import (
	"reflect"
	"testing"

	"github.com/JaimeStill/spike-messaging/services/exercise/domain/exercise/rules"
)

func TestObserve(t *testing.T) {
	tests := []struct {
		name       string
		elements   []rules.Element
		holders    map[string]string
		own        []string
		contacts   []string
		objectives []rules.ObjectiveStatus
	}{
		{
			name: "a squad sees an enemy one cell away",
			elements: []rules.Element{
				squad("r1", "red", loc("b", 0, 0)), squad("b1", "blue", loc("b", 1, 0)),
			},
			own: []string{"r1"}, contacts: []string{"b1"},
		},
		{
			name: "a squad does not see an enemy two cells away",
			elements: []rules.Element{
				squad("r1", "red", loc("b", 0, 0)), squad("b1", "blue", loc("b", 2, 0)),
			},
			own: []string{"r1"}, contacts: []string{},
		},
		{
			name: "a scout sees an enemy two cells away",
			elements: []rules.Element{
				scout("r1", "red", loc("b", 0, 1)), squad("b1", "blue", loc("b", 2, 1)),
			},
			own: []string{"r1"}, contacts: []string{"b1"},
		},
		{
			name: "a scout does not see an enemy three cells away",
			elements: []rules.Element{
				scout("r1", "red", loc("b", 0, 1)), squad("b1", "blue", loc("b", 3, 1)),
			},
			own: []string{"r1"}, contacts: []string{},
		},
		{
			name: "sight reaches the diagonal corner at the same distance",
			elements: []rules.Element{
				scout("r1", "red", loc("b", 1, 1)), squad("b1", "blue", loc("b", 3, 3)),
			},
			own: []string{"r1"}, contacts: []string{"b1"},
		},
		{
			name: "sight does not cross the sector's edge",
			elements: []rules.Element{
				squad("r1", "red", loc("a", 1, 5)), squad("b1", "blue", loc("b", 1, 5)),
			},
			own: []string{"r1"}, contacts: []string{},
		},
		{
			name: "sight does not cross a gate",
			elements: []rules.Element{
				scout("r1", "red", loc("a", 5, 5)), squad("b1", "blue", loc("b", 1, 0)),
			},
			own: []string{"r1"}, contacts: []string{},
		},
		{
			name: "any own element's sight counts, and every list is sorted",
			elements: []rules.Element{
				squad("r2", "red", loc("b", 5, 4)),
				squad("r1", "red", loc("b", 0, 0)),
				squad("b2", "blue", loc("b", 4, 3)),
				squad("b1", "blue", loc("b", 1, 1)),
				squad("b3", "blue", loc("a", 0, 0)),
			},
			holders:    map[string]string{"b:5,5": "blue"},
			own:        []string{"r1", "r2"},
			contacts:   []string{"b1", "b2"},
			objectives: []rules.ObjectiveStatus{{At: loc("b", 5, 5), Holder: "blue"}},
		},
		{
			name: "an objective in sight reports that it is unheld",
			elements: []rules.Element{
				squad("r1", "red", loc("a", 3, 1)), squad("b1", "blue", loc("b", 0, 5)),
			},
			own:        []string{"r1"},
			contacts:   []string{},
			objectives: []rules.ObjectiveStatus{{At: loc("a", 3, 0), Holder: ""}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := state(tt.elements...)
			if tt.holders != nil {
				s.Holders = tt.holders
			}
			obs := rules.Observe(s, 7)
			red := obs[0]
			if red.Faction != "red" || red.Round != 7 || obs[1].Faction != "blue" {
				t.Errorf("observations = %q round %d, %q", red.Faction, red.Round, obs[1].Faction)
			}
			if got := ids(red.Own); !reflect.DeepEqual(got, tt.own) {
				t.Errorf("own = %v, want %v", got, tt.own)
			}
			if got := ids(red.Contacts); !reflect.DeepEqual(got, tt.contacts) {
				t.Errorf("contacts = %v, want %v", got, tt.contacts)
			}
			want := tt.objectives
			if want == nil {
				want = []rules.ObjectiveStatus{}
			}
			if !reflect.DeepEqual(red.Objectives, want) {
				t.Errorf("objectives = %+v, want %+v", red.Objectives, want)
			}
		})
	}
}

// An observation carries each element's kind, health, strength, and status.
func TestObserveCarriesKindHealthAndStatus(t *testing.T) {
	want := engaged(hurt(squad("b1", "blue", loc("b", 1, 0)), 80, 35))
	s := state(engaged(squad("r1", "red", loc("b", 1, 0))), want)
	obs := rules.Observe(s, 0)
	if len(obs[0].Contacts) != 1 || !reflect.DeepEqual(obs[0].Contacts[0], want) {
		t.Errorf("contacts = %+v, want [%+v]", obs[0].Contacts, want)
	}
}
