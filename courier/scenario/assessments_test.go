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
		for _, w := range []string{
			"round 1 red: own r1 a:0,0 · r2 a:4,4 | contacts b1 a:12,2 seen 1 age 2 | objectives a:0,0 red · a:0,4 unheld age 1 · b:3,3 unknown",
			"round 2 red: own r1 a:0,0 · r2 a:4,4 | contacts none",
			"red dropped b1",
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

// With --summary, the narration groups each round's assessments, a block
// per faction: its elements and what it lost, the contacts it sees and
// remembers, those dropped, and each objective by what the faction believes
// of it. Rounds in which only ages advanced collapse into one line, and a
// round a faction was never assessed on does not hold back the rest.
func TestAssessmentsSummarizes(t *testing.T) {
	b := memory.New()
	at := func(s string, x, y int) map[string]any { return map[string]any{"sector": s, "x": x, "y": y} }
	own := func(id string, strength int) map[string]any {
		return map[string]any{"id": id, "strength": strength, "at": at("a", 0, 0)}
	}
	contact := func(id string, strength, x, seen, age int) map[string]any {
		return map[string]any{"id": id, "strength": strength, "at": at("a", x, 5), "seen": seen, "age": age}
	}
	objectives := func(age int) []any {
		return []any{
			map[string]any{"at": at("a", 0, 0), "holder": "red", "known": true},
			map[string]any{"at": at("a", 0, 4), "holder": "blue", "known": true, "seen": 0, "age": age},
			map[string]any{"at": at("a", 4, 4), "known": true},
			map[string]any{"at": at("b", 3, 3), "known": false},
		}
	}
	assess := func(faction string, round int, own []any, contacts ...any) {
		publishJSON(t, b, faction+strconv.Itoa(round), "intelligence.assessment.issued", map[string]any{
			"exercise": exerciseID, "faction": faction, "round": round,
			"own": own, "contacts": contacts, "objectives": objectives(round),
		})
	}
	publishJSON(t, b, "s", "exercise.started", map[string]any{"exercise": exerciseID, "factions": []string{"red", "blue"}})
	blue := []any{own("b1", 2)}
	// Blue arrives ahead of red, and red's round 2 never arrives.
	assess("blue", 0, blue)
	assess("red", 0, []any{own("r1", 4), own("r2", 3), own("r3", 1)}, contact("b2", 3, 5, 0, 0))
	assess("blue", 1, blue)
	assess("red", 1, []any{own("r1", 4), own("r2", 1)}, contact("b2", 3, 5, 0, 1))
	assess("blue", 2, blue)
	assess("blue", 3, blue)
	assess("red", 3, []any{own("r1", 4), own("r2", 1)})
	assess("blue", 4, blue)
	assess("red", 4, []any{own("r1", 4), own("r2", 1)})
	assess("blue", 5, blue)
	assess("red", 5, []any{own("r1", 4), own("r2", 1)})
	publishJSON(t, b, "c", "exercise.concluded", map[string]any{"exercise": exerciseID, "round": 5, "reason": "limit"})
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
			"  round 0\n" +
				"    red   3 elements, strength 8\n" +
				"          sees       b2(3) a:5,5\n" +
				"          objectives held a:0,0 · blue a:0,4 · unheld a:4,4 · unknown b:3,3\n" +
				"    blue  1 element, strength 2\n",
			"    red   2 elements, strength 5 (-3, lost r3)\n" +
				"          sees       none\n" +
				"          remembers  b2(3) at a:5,5, 1 round ago\n" +
				"          objectives held a:0,0 · blue a:0,4 (1 round ago) · unheld a:4,4 · unknown b:3,3\n",
			"    red   no assessment of this round",
			"          dropped    b2, last seen 3 rounds ago",
			"concluded after round 5 with no winner: limit",
		} {
			if !strings.Contains(got, w) {
				t.Errorf("narration lacks %q:\n%s", w, got)
			}
		}
		// After red's drop, both pictures change only in the objective's age.
		if !strings.Contains(got, "rounds 4–5: no change") || strings.Contains(got, "round 4\n") {
			t.Errorf("rounds 4 and 5 are not collapsed:\n%s", got)
		}
		t.Log("\n" + got)
	}
}

// Rounds in which no faction's picture changes collapse into one line.
func TestAssessmentsSummaryCollapsesQuietRounds(t *testing.T) {
	b := memory.New()
	publishJSON(t, b, "s", "exercise.started", map[string]any{"exercise": exerciseID, "factions": []string{"red", "blue"}})
	for r := range 5 {
		for _, f := range []string{"red", "blue"} {
			publishJSON(t, b, f+strconv.Itoa(r), "intelligence.assessment.issued", map[string]any{
				"exercise": exerciseID, "faction": f, "round": r,
				"own": []any{map[string]any{"id": f + "1", "strength": 2, "at": map[string]any{"sector": "a", "x": 0, "y": 0}}},
			})
		}
	}
	publishJSON(t, b, "c", "exercise.concluded", map[string]any{"exercise": exerciseID, "round": 4, "reason": "limit"})
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
		if !strings.Contains(got, "  round 0\n") || !strings.Contains(got, "rounds 1–4: no change") || strings.Contains(got, "round 1\n") {
			t.Errorf("narration:\n%s", got)
		}
	}
}

// A faction's first round that arrives after the other faction's later one
// is still narrated, in order: the summary begins at round 0, not at the
// first round it receives.
func TestAssessmentsSummaryNarratesALateFirstRound(t *testing.T) {
	b := memory.New()
	publishJSON(t, b, "s", "exercise.started", map[string]any{"exercise": exerciseID, "factions": []string{"red", "blue"}})
	assess := func(f string, r int) {
		publishJSON(t, b, f+strconv.Itoa(r), "intelligence.assessment.issued", map[string]any{
			"exercise": exerciseID, "faction": f, "round": r,
			"own": []any{map[string]any{"id": f + strconv.Itoa(r), "strength": 1, "at": map[string]any{"sector": "a", "x": 0, "y": 0}}},
		})
	}
	// Red's round 0 is never assessed, and blue's arrives after red's round 1.
	assess("red", 1)
	assess("blue", 0)
	assess("blue", 1)
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
		r0, r1 := strings.Index(got, "  round 0\n"), strings.Index(got, "  round 1\n")
		if r0 < 0 || r1 < r0 || !strings.Contains(got, "red   no assessment of this round") {
			t.Errorf("narration:\n%s", got)
		}
	}
}

// Each round's block ends with the directives decided on it: each element
// whose directive changed, with what it was doing before, and none for an
// element whose directive stands. A directive without a rule, as courier's
// stand-in issues, heads for its target. Unsummarized, a directive is
// narrated as it arrives.
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
		for _, args := range [][]string{{"--summary"}, {}} {
			rep, out := reporter()
			cmd := scenario.Command(s, func() *scenario.Reporter { return rep })
			cmd.SetArgs(append([]string{"--exercise", exerciseID, "--wait", "5s"}, args...))
			if err := cmd.ExecuteContext(t.Context()); err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
			got := out.String()
			want := []string{
				"round 0 red directs: r1 secures a:4,4 · r2 holds",
				"round 1 red directs: r1 engages b2 at a:5,5 (was securing a:4,4)\n",
				"round 0 blue directs: b1 heads for a:0,4",
			}
			if len(args) > 0 {
				want = []string{
					"  round 0\n" +
						"    red   1 element, strength 2\n" +
						"          sees       none\n" +
						"          objectives none\n" +
						"          directs    r1 secures a:4,4\n" +
						"                     r2 holds\n" +
						"    blue  1 element, strength 2\n" +
						"          sees       none\n" +
						"          objectives none\n" +
						"          directs    b1 heads for a:0,4\n",
					"  round 1\n" +
						"    red   1 element, strength 2\n" +
						"          sees       none\n" +
						"          objectives none\n" +
						"          directs    r1 engages b2 at a:5,5 (was securing a:4,4)\n" +
						"    blue  1 element, strength 2\n",
					"round 2: no change",
				}
			}
			for _, w := range want {
				if !strings.Contains(got, w) {
					t.Errorf("%v: narration lacks %q:\n%s", args, w, got)
				}
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
