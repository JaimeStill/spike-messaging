package scenario_test

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JaimeStill/spike-messaging/courier/scenario"
	"github.com/JaimeStill/spike-messaging/messaging"
	"github.com/JaimeStill/spike-messaging/messaging/memory"
)

// On a stream of assessments, a repeat, a stale one, another exercise's and
// a conclusion that precedes the last round's assessments, the scenario
// narrates each faction's rounds in order, notes the contact a later
// assessment drops, waits for the concluded round, and releases once.
func TestAssessmentsNarratesUntilConcluded(t *testing.T) {
	b := memory.New()
	at := func(s string, x, y int) map[string]any { return map[string]any{"sector": s, "x": x, "y": y} }
	contact := func(id string, x, y, seen, age int) map[string]any {
		return map[string]any{"id": id, "at": at("a", x, y), "seen": seen, "age": age}
	}
	assess := func(id, ex, faction string, round int, contacts ...any) {
		publishJSON(t, b, id, "intelligence.assessment.issued", map[string]any{
			"exercise": ex, "faction": faction, "round": round,
			"own":      []any{map[string]any{"id": "r1", "at": at("a", 0, 0)}, map[string]any{"id": "r2", "at": at("a", 4, 4)}},
			"contacts": contacts,
			"objectives": []any{
				map[string]any{"at": at("a", 0, 0), "holder": faction, "known": true},
				map[string]any{"at": at("a", 0, 4), "known": true, "age": 1},
				map[string]any{"at": at("b", 3, 3), "known": false},
			},
		})
	}
	assess("x", "another", "red", 1)
	assess("r1", exerciseID, "red", 1, contact("b1", 12, 2, 1, 2))
	assess("r1-again", exerciseID, "red", 1, contact("b9", 1, 1, 1, 0))
	assess("r2", exerciseID, "red", 2)
	assess("r1-stale", exerciseID, "red", 1, contact("b8", 1, 1, 1, 0))
	assess("u2", exerciseID, "blue", 2)
	publishJSON(t, b, "c", "exercise.concluded", map[string]any{"exercise": exerciseID, "round": 3, "winner": "red", "reason": "objectives"})
	assess("r3", exerciseID, "red", 3)
	assess("u3", exerciseID, "blue", 3)

	var released atomic.Int32
	joins := func(stream, prefix string, _ time.Duration) (messaging.Broker, func() error, error) {
		if stream != "exercise" || prefix != "exercise" {
			t.Errorf("joined %s/%s, want the services' default stream", stream, prefix)
		}
		return b, func() error { released.Add(1); return nil }, nil
	}
	for _, s := range scenario.Scenarios(scenario.Dependencies{Joins: joins}) {
		if s.Name != "assessments" {
			continue
		}
		rep, out := reporter()
		cmd := scenario.Command(s, func() *scenario.Reporter { return rep })
		cmd.SetArgs([]string{"--exercise", exerciseID, "--wait", "5s"})
		if err := cmd.ExecuteContext(t.Context()); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		got := out.String()
		for _, w := range []string{
			"round 1 red: own r1 a:0,0 · r2 a:4,4 | contacts b1 a:12,2 seen 1 age 2 | objectives a:0,0 red · a:0,4 unheld age 1 · b:3,3 unknown",
			"round 2 red: own r1 a:0,0 · r2 a:4,4 | contacts none",
			"dropped b1",
			"round 2 blue:", "round 3 red:", "round 3 blue:",
			"concluded after round 3: red wins by objectives", "drained cleanly",
		} {
			if !strings.Contains(got, w) {
				t.Errorf("narration lacks %q:\n%s", w, got)
			}
		}
		for _, w := range []string{"b9", "b8", "another"} {
			if strings.Contains(got, w) {
				t.Errorf("narration holds %q, which is stale or another exercise's:\n%s", w, got)
			}
		}
		if n := strings.Count(got, "round 1 red:"); n != 1 {
			t.Errorf("narrated round 1 red %d times, want once", n)
		}
		if n := released.Load(); n != 1 {
			t.Errorf("released %d times, want once", n)
		}
		t.Log("\n" + got)
	}
}

// With --faction, the other faction's assessments are not narrated, and a
// conclusion without a winner is noted as such.
func TestAssessmentsNarratesOneFaction(t *testing.T) {
	b := memory.New()
	for i, f := range []string{"blue", "red"} {
		publishJSON(t, b, f, "intelligence.assessment.issued", map[string]any{"exercise": exerciseID, "faction": f, "round": i})
	}
	publishJSON(t, b, "c", "exercise.concluded", map[string]any{"exercise": exerciseID, "round": 1, "reason": "rounds"})
	joins := func(string, string, time.Duration) (messaging.Broker, func() error, error) { return b, nil, nil }
	for _, s := range scenario.Scenarios(scenario.Dependencies{Joins: joins}) {
		if s.Name != "assessments" {
			continue
		}
		rep, out := reporter()
		cmd := scenario.Command(s, func() *scenario.Reporter { return rep })
		cmd.SetArgs([]string{"--exercise", exerciseID, "--faction", "red", "--wait", "5s"})
		if err := cmd.ExecuteContext(t.Context()); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		got := out.String()
		if !strings.Contains(got, "round 1 red:") || strings.Contains(got, "blue") || !strings.Contains(got, "with no winner: rounds") {
			t.Errorf("narration:\n%s", got)
		}
	}
}

// With --summary, each assessment is narrated as counts: own elements and
// their strength, contacts by ID, strength, and age, and objectives by
// holder. An own element a later assessment lacks is noted as lost.
func TestAssessmentsSummarizes(t *testing.T) {
	b := memory.New()
	at := func(s string, x, y int) map[string]any { return map[string]any{"sector": s, "x": x, "y": y} }
	own := func(id string, strength int) map[string]any {
		return map[string]any{"id": id, "strength": strength, "at": at("a", 0, 0)}
	}
	objectives := []any{
		map[string]any{"at": at("a", 0, 0), "holder": "red", "known": true},
		map[string]any{"at": at("a", 0, 4), "holder": "blue", "known": true},
		map[string]any{"at": at("a", 4, 4), "known": true},
		map[string]any{"at": at("b", 3, 3), "known": false},
	}
	publishJSON(t, b, "r0", "intelligence.assessment.issued", map[string]any{
		"exercise": exerciseID, "faction": "red", "round": 0,
		"own":        []any{own("r1", 4), own("r2", 3), own("r3", 1)},
		"contacts":   []any{map[string]any{"id": "b2", "strength": 3, "at": at("a", 5, 5), "seen": 0, "age": 0}},
		"objectives": objectives,
	})
	publishJSON(t, b, "r1", "intelligence.assessment.issued", map[string]any{
		"exercise": exerciseID, "faction": "red", "round": 1,
		"own":        []any{own("r1", 4), own("r2", 1)},
		"contacts":   []any{map[string]any{"id": "b2", "strength": 3, "at": at("a", 5, 5), "seen": 0, "age": 1}},
		"objectives": objectives,
	})
	publishJSON(t, b, "c", "exercise.concluded", map[string]any{"exercise": exerciseID, "round": 1, "reason": "limit"})
	joins := func(string, string, time.Duration) (messaging.Broker, func() error, error) { return b, nil, nil }
	for _, s := range scenario.Scenarios(scenario.Dependencies{Joins: joins}) {
		if s.Name != "assessments" {
			continue
		}
		rep, out := reporter()
		cmd := scenario.Command(s, func() *scenario.Reporter { return rep })
		cmd.SetArgs([]string{"--exercise", exerciseID, "--summary", "--wait", "5s"})
		if err := cmd.ExecuteContext(t.Context()); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		got := out.String()
		for _, w := range []string{
			"round 0 red: own 3, strength 8 | contacts b2(3) age 0 | objectives blue 1 · red 1 · unheld 1 · unknown 1",
			"round 1 red: own 2, strength 5 | contacts b2(3) age 1 |",
			"lost r3",
		} {
			if !strings.Contains(got, w) {
				t.Errorf("narration lacks %q:\n%s", w, got)
			}
		}
	}
}

func TestAssessmentsValidatesItsFlags(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"--exercise", "nope"},
		{"--exercise", exerciseID, "--wait", "0s"},
	} {
		if _, err := execute(t, "assessments", args...); !strings.Contains(err.Error(), "usage") {
			t.Errorf("%v: err = %v, want a usage error", args, err)
		}
	}
}
