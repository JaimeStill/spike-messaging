package scenario_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/JaimeStill/spike-messaging/courier/scenario"
	"github.com/JaimeStill/spike-messaging/messaging"
	"github.com/JaimeStill/spike-messaging/messaging/memory"
)

// observer stands in for exercise's API: one round, round 0, in which red's
// squad sees the objective at a:1,1 and blue's squad sees nothing.
func observer(t *testing.T) *httptest.Server {
	at := func(x, y int) map[string]any { return map[string]any{"sector": "a", "x": x, "y": y} }
	history := []any{map[string]any{
		"round": 0,
		"state": map[string]any{
			"map":      map[string]any{"sectors": []any{map[string]any{"id": "a", "objectives": []any{map[string]any{"x": 1, "y": 1}}}}},
			"factions": []string{"red", "blue"},
			"holders":  map[string]string{},
		},
		"observations": []any{
			map[string]any{"own": []any{squadAt("r1", at(0, 0))}, "contacts": []any{},
				"objectives": []any{map[string]any{"at": at(1, 1), "holder": ""}}},
			map[string]any{"own": []any{squadAt("b1", at(4, 4))}, "contacts": []any{}, "objectives": []any{}},
		},
	}}
	mux := http.NewServeMux()
	serve := func(path string, v any) {
		mux.HandleFunc("GET "+path, func(w http.ResponseWriter, _ *http.Request) {
			if err := json.NewEncoder(w).Encode(v); err != nil {
				t.Error(err)
			}
		})
	}
	serve("/api/exercises/"+exerciseID+"/history", history)
	serve("/api/exercises/"+exerciseID, map[string]any{"verdict": map[string]any{"winner": "red", "reason": "limit"}})
	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func squadAt(id string, at map[string]any) map[string]any {
	return map[string]any{"id": id, "kind": "squad", "strength": 100, "health": []int{100}, "status": "ready", "at": at}
}

// The theater-check scenario reads an exercise's assessments from the
// stream's beginning, and the observer's record from exercise's API, and
// reconciles them: it passes when they agree, and fails on an
// inconsistency.
func TestTheaterCheckReconcilesAnExercise(t *testing.T) {
	at := map[string]any{"sector": "a", "x": 0, "y": 0}
	for _, tc := range []struct {
		name     string
		redHolds string
		want     []string
		fails    bool
	}{
		{"consistent", "", []string{
			"  theater  " + exerciseID + "\n    rounds          1\n    assessments     2, 0 revised\n    contact rounds  3\n",
			"  consistent  every assessment matches",
			"  observer\n    verdict   red by limit\n    holds     none\n",
			"  red\n    believes  objective:1,1  unheld  truly unheld\n",
			"  blue\n    believes  objective:1,1  undiscovered  truly unheld",
		}, false},
		{"inconsistent", "blue", []string{
			"  inconsistencies  1\n    round 0 red: objective:1,1 is in sight and unheld, but not reported so\n",
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := memory.New()
			objective := map[string]any{"at": map[string]any{"sector": "a", "x": 1, "y": 1}, "holder": tc.redHolds, "known": true, "seen": 0, "age": 0}
			publishJSON(t, b, "ar", "intelligence.assessment.issued", map[string]any{
				"exercise": exerciseID, "faction": "red", "round": 0,
				"own": []any{squadAt("r1", at)}, "contacts": []any{}, "objectives": []any{objective}})
			publishJSON(t, b, "ab", "intelligence.assessment.issued", map[string]any{
				"exercise": exerciseID, "faction": "blue", "round": 0,
				"own": []any{squadAt("b1", map[string]any{"sector": "a", "x": 4, "y": 4})}, "contacts": []any{}, "objectives": []any{}})
			publishJSON(t, b, "ao", "intelligence.assessment.issued", map[string]any{"exercise": "another", "faction": "red", "round": 5})
			publishJSON(t, b, "c", "exercise.concluded", map[string]any{"exercise": exerciseID, "round": 0, "winner": "red", "reason": "limit"})
			released := false
			joins := func(string, string, time.Duration) (messaging.Broker, func() error, error) {
				return b, func() error { released = true; return nil }, nil
			}
			for _, s := range scenario.Scenarios(scenario.Dependencies{Joins: joins}) {
				if s.Name != "theater-check" {
					continue
				}
				rep, out := reporter()
				cmd := scenario.Command(s, func() *scenario.Reporter { return rep })
				cmd.SetArgs([]string{"--exercise", exerciseID, "--exercise-url", observer(t).URL, "--wait", "5s", "--idle", "50ms"})
				err := cmd.ExecuteContext(t.Context())
				if tc.fails != (err != nil) {
					t.Fatalf("err = %v, want failure %v\n%s", err, tc.fails, out)
				}
				if tc.fails && !strings.Contains(err.Error(), "1 inconsistencies") {
					t.Errorf("err = %v", err)
				}
				got := out.String()
				for _, w := range tc.want {
					if !strings.Contains(got, w) {
						t.Errorf("report lacks %q:\n%s", w, got)
					}
				}
				if !released {
					t.Error("the stream was not released")
				}
			}
		})
	}
}
