package scenario_test

import (
	"strconv"
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
		golden(t, "assessments-until-concluded", got)
		for _, w := range []string{"b9", "b8", "another"} {
			if strings.Contains(got, w) {
				t.Errorf("narration holds %q, which is stale or another exercise's:\n%s", w, got)
			}
		}
		if n := released.Load(); n != 1 {
			t.Errorf("released %d times, want once", n)
		}
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
		golden(t, "assessments-one-faction", out.String())
	}
}

// A directive is narrated as it arrives: each squad whose directive
// changed, in the theater's verbs, and none whose directive stands.
// command's secure rule reads as a capture, and a directive without a rule,
// as courier's stand-in issues, heads for its target.
func TestAssessmentsNarratesDirectives(t *testing.T) {
	b := memory.New()
	at := func(x, y int) map[string]any { return map[string]any{"sector": "a", "x": x, "y": y} }
	publishJSON(t, b, "s", "exercise.started", map[string]any{"exercise": exerciseID, "factions": []string{"red", "blue"}})
	for r := range 3 {
		for _, f := range []string{"red", "blue"} {
			publishJSON(t, b, f+strconv.Itoa(r), "intelligence.assessment.issued", map[string]any{
				"exercise": exerciseID, "faction": f, "round": r,
				"own": []any{map[string]any{"id": f[:1] + "1", "strength": 2, "at": at(0, 0)}},
			})
		}
		direct := func(f string, ds ...any) {
			publishJSON(t, b, "d"+f+strconv.Itoa(r), "command.directive.issued", map[string]any{
				"exercise": exerciseID, "faction": f, "round": r, "directives": ds,
			})
		}
		switch r {
		case 0:
			direct("red",
				map[string]any{"element": "r1", "rule": "secure", "target": at(4, 4)},
				map[string]any{"element": "r2", "rule": "hold", "target": nil})
			direct("blue", map[string]any{"element": "b1", "target": at(0, 4)})
		case 1:
			direct("red",
				map[string]any{"element": "r1", "rule": "engage", "contact": "b2", "target": at(5, 5)},
				map[string]any{"element": "r2", "rule": "hold", "target": nil})
		}
	}
	publishJSON(t, b, "c", "exercise.concluded", map[string]any{"exercise": exerciseID, "round": 2, "reason": "limit"})
	joins := func(string, string, time.Duration) (messaging.Broker, func() error, error) { return b, nil, nil }
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
		golden(t, "assessments-directives", out.String())
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
