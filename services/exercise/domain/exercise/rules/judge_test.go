package rules_test

import (
	"testing"

	"github.com/JaimeStill/spike-messaging/services/exercise/domain/exercise/rules"
)

func TestJudge(t *testing.T) {
	red := squad("r1", "red", loc("a", 0, 0))
	blue := squad("b1", "blue", loc("b", 0, 5))
	tests := []struct {
		name     string
		elements []rules.Element
		holders  map[string]string
		round    int
		want     rules.Verdict
	}{
		{
			name:     "both factions standing before the limit is not over",
			elements: []rules.Element{red, blue},
			holders:  map[string]string{"a:3,0": "red"},
			round:    3,
			want:     rules.Verdict{},
		},
		{
			name:     "a faction with no elements loses",
			elements: []rules.Element{red},
			round:    3,
			want:     rules.Verdict{Over: true, Winner: "red", Reason: "elimination"},
		},
		{
			name:     "elimination outranks holding every objective",
			elements: []rules.Element{blue},
			holders:  map[string]string{"a:3,0": "red", "b:5,5": "red"},
			round:    3,
			want:     rules.Verdict{Over: true, Winner: "blue", Reason: "elimination"},
		},
		{
			name:  "both factions eliminated is a draw",
			round: 3,
			want:  rules.Verdict{Over: true, Reason: "elimination"},
		},
		{
			name:     "a faction holding every objective wins",
			elements: []rules.Element{red, blue},
			holders:  map[string]string{"a:3,0": "blue", "b:5,5": "blue"},
			round:    3,
			want:     rules.Verdict{Over: true, Winner: "blue", Reason: "objectives"},
		},
		{
			name:     "at the limit the faction holding more objectives wins",
			elements: []rules.Element{red, blue},
			holders:  map[string]string{"a:3,0": "red"},
			round:    10,
			want:     rules.Verdict{Over: true, Winner: "red", Reason: "limit"},
		},
		{
			name:     "at the limit equal holdings are a draw",
			elements: []rules.Element{red, blue},
			holders:  map[string]string{"a:3,0": "red", "b:5,5": "blue"},
			round:    10,
			want:     rules.Verdict{Over: true, Reason: "limit"},
		},
		{
			name:     "past the limit is judged as at it",
			elements: []rules.Element{red, blue},
			round:    11,
			want:     rules.Verdict{Over: true, Reason: "limit"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := state(tt.elements...)
			if tt.holders != nil {
				s.Holders = tt.holders
			}
			if got := rules.Judge(s, tt.round, 10); got != tt.want {
				t.Errorf("Judge = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestResolveJudgesAfterCapture(t *testing.T) {
	s := state(
		squad("r1", "red", loc("a", 3, 1)),
		squad("b1", "blue", loc("a", 0, 5)),
	)
	s.Holders["b:5,5"] = "red"
	s, _, v, _ := rules.Resolve(s, seed, 1, 10, []rules.Order{order("r1", loc("a", 3, 0))})
	if v.Over {
		t.Fatalf("round 1: %+v, want not over before the capture", v)
	}
	_, _, v, _ = rules.Resolve(s, seed, 2, 10, nil)
	if want := (rules.Verdict{Over: true, Winner: "red", Reason: "objectives"}); v != want {
		t.Errorf("verdict = %+v, want %+v", v, want)
	}
}
