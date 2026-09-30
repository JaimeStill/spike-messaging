package scenario_test

import (
	"strings"
	"testing"
	"time"

	"github.com/JaimeStill/spike-messaging/courier/scenario"
	"github.com/JaimeStill/spike-messaging/messaging"
	"github.com/JaimeStill/spike-messaging/messaging/memory"
)

// The theater scenario joins the stream, waits for the start, narrates
// until the exercise concludes and its final assessments are in, then
// states the final conditions and releases.
func TestTheaterNarratesAnExercise(t *testing.T) {
	b := memory.New()
	at := map[string]any{"sector": "a", "x": 0, "y": 0}
	publishJSON(t, b, "s", "exercise.started", map[string]any{
		"exercise": exerciseID, "name": "tiny", "factions": []string{"red", "blue"},
		"round_interval_ms": 1000, "round_limit": 1, "seed": 7,
		"map": map[string]any{"sectors": []any{map[string]any{"id": "a", "width": 2, "height": 1, "objectives": []any{}}}},
	})
	for _, f := range []string{"red", "blue"} {
		publishJSON(t, b, "o"+f, "exercise.round.observed", map[string]any{
			"exercise": exerciseID, "faction": f, "round": 0,
			"own": []any{map[string]any{"id": f[:1] + "1", "kind": "squad", "strength": 100, "health": []int{100}, "at": at}},
		})
	}
	publishJSON(t, b, "r1", "exercise.round.resolved", map[string]any{
		"exercise": exerciseID, "round": 1, "retreats": []any{}, "engagements": []any{}, "captures": []any{}, "progress": []any{}, "objectives": []any{},
		"losses": []any{map[string]any{"id": "b1", "faction": "blue"}},
	})
	publishJSON(t, b, "c", "exercise.concluded", map[string]any{"exercise": exerciseID, "round": 1, "winner": "red", "reason": "elimination"})
	for _, f := range []string{"red", "blue"} {
		publishJSON(t, b, "a"+f, "intelligence.assessment.issued", map[string]any{"exercise": exerciseID, "faction": f, "round": 1})
	}
	joins := func(string, string, time.Duration) (messaging.Broker, func() error, error) { return b, nil, nil }
	for _, s := range scenario.Scenarios(scenario.Dependencies{Joins: joins}) {
		if s.Name != "theater" {
			continue
		}
		rep, out := reporter()
		cmd := scenario.Command(s, func() *scenario.Reporter { return rep })
		cmd.SetArgs([]string{"--exercise", exerciseID, "--wait", "5s"})
		start := time.Now()
		if err := cmd.ExecuteContext(t.Context()); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		if d := time.Since(start); d > 2*time.Second {
			t.Errorf("the run took %s: it waited out the settle instead of the final assessments", d)
		}
		got := out.String()
		for _, w := range []string{
			"tiny: 1 rounds at 1s",
			"    seed        7",
			"  1  exercise      blue\n    b1 destroyed\n",
			"  verdict  red wins by elimination after round 1",
			"  check  mise run demo-theater-check " + exerciseID,
		} {
			if !strings.Contains(got, w) {
				t.Errorf("narration lacks %q:\n%s", w, got)
			}
		}
	}
}
