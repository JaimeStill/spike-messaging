package scenario

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/JaimeStill/spike-messaging/core/event"
)

const theaterID = "01999f4e-6a3b-7c2d-8e1f-0a1b2c3d4e5f"

// script feeds a narrator events, each at its offset from a start time,
// and collects what it narrates.
type script struct {
	t     *testing.T
	n     *narrator
	t0    time.Time
	lines []string
}

func newScript(t *testing.T) *script {
	s := &script{t: t, n: newNarrator(theaterID), t0: time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)}
	s.n.note = func(format string, args ...any) { s.lines = append(s.lines, fmt.Sprintf(format, args...)) }
	return s
}

// at hands the narrator an event of typ with data, ms after the start.
func (s *script) at(ms int, typ string, data map[string]any) {
	s.t.Helper()
	data["exercise"] = theaterID
	body, err := json.Marshal(data)
	if err != nil {
		s.t.Fatal(err)
	}
	e := eventOf(typ, body, s.t0.Add(time.Duration(ms)*time.Millisecond))
	if err := s.n.handle(e); err != nil {
		s.t.Fatal(err)
	}
}

func eventOf(typ string, data []byte, at time.Time) event.Event {
	return event.Event{ID: typ + at.String(), Source: "/test", Type: typ, Time: at, DataContentType: "application/json", Data: data}
}

func cell(sector string, x, y int) map[string]any {
	return map[string]any{"sector": sector, "x": x, "y": y}
}

func sq(id, kind string, strength int, at map[string]any) map[string]any {
	return map[string]any{"id": id, "kind": kind, "strength": strength, "at": at}
}

// The narrator tells the initial conditions once the start and both round-0
// observations are in, then one line for each event that changes something
// (a sighting, a changed directive, a new set of moving squads, a fight, a
// loss, a capture, an objective seen held) and nothing for one that
// changes nothing. Its final conditions give the verdict, the holders, the
// survivors and losses, the events by type, and the median of each hop.
func TestNarratorTellsWhatChanges(t *testing.T) {
	s := newScript(t)
	s.at(0, startedType, map[string]any{
		"name": "skirmish", "factions": []string{"red", "blue"}, "round_interval_ms": 1000, "round_limit": 3,
		"map": map[string]any{"sectors": []any{
			map[string]any{"id": "a", "width": 5, "height": 5, "objectives": []any{map[string]any{"x": 4, "y": 4}}},
			map[string]any{"id": "b", "width": 3, "height": 3, "objectives": []any{map[string]any{"x": 1, "y": 1}}},
		}},
	})
	// An assessment arriving before the initial conditions waits for them.
	s.at(1, assessmentType, map[string]any{"faction": "blue", "round": 0,
		"objectives": []any{map[string]any{"at": cell("a", 4, 4), "holder": "", "known": true}}})
	s.at(2, observedType, map[string]any{"faction": "red", "round": 0, "own": []any{
		sq("r1", "force", 4, cell("a", 0, 0)), sq("r2", "scout", 1, cell("a", 0, 1))}})
	s.at(3, observedType, map[string]any{"faction": "blue", "round": 0, "own": []any{
		sq("b1", "force", 3, cell("a", 4, 0))}})
	s.at(10, assessmentType, map[string]any{"faction": "red", "round": 0,
		"objectives": []any{map[string]any{"at": cell("a", 4, 4), "known": false}}})
	s.at(20, directiveType, map[string]any{"faction": "red", "round": 0, "directives": []any{
		map[string]any{"element": "r1", "rule": "secure", "target": cell("a", 4, 4)},
		map[string]any{"element": "r2", "rule": "hold", "target": nil}}})
	s.at(30, ordersType, map[string]any{"faction": "red", "round": 1, "orders": []any{
		map[string]any{"element": "r1", "steps": []any{cell("a", 1, 0)}}}})

	s.at(1000, resolvedType, map[string]any{"round": 1, "engagements": []any{}, "captures": []any{}, "losses": []any{}})
	s.at(1001, observedType, map[string]any{"faction": "red", "round": 1, "own": []any{
		sq("r1", "force", 4, cell("a", 1, 0)), sq("r2", "scout", 1, cell("a", 0, 1))}})
	s.at(1011, assessmentType, map[string]any{"faction": "red", "round": 1,
		"contacts":   []any{map[string]any{"id": "b1", "strength": 3, "at": cell("a", 3, 0), "seen": 1, "age": 0}},
		"objectives": []any{map[string]any{"at": cell("a", 4, 4), "known": false}}})
	s.at(1005, ordersType, map[string]any{"faction": "red", "round": 2, "orders": []any{
		map[string]any{"element": "r1", "steps": []any{cell("a", 2, 0)}}}})
	s.at(1026, directiveType, map[string]any{"faction": "red", "round": 1, "directives": []any{
		map[string]any{"element": "r1", "rule": "engage", "contact": "b1", "target": cell("a", 3, 0)},
		map[string]any{"element": "r2", "rule": "hold", "target": nil}}})
	s.at(1041, ordersType, map[string]any{"faction": "red", "round": 2, "orders": []any{
		map[string]any{"element": "r1", "steps": []any{cell("a", 2, 0)}},
		map[string]any{"element": "r2", "steps": []any{cell("a", 0, 2)}}}})

	s.at(2000, resolvedType, map[string]any{"round": 2, "captures": []any{}, "losses": []any{},
		"engagements": []any{map[string]any{"at": cell("a", 3, 0), "elements": []any{
			map[string]any{"id": "b1", "faction": "blue", "before": 3, "after": 1},
			map[string]any{"id": "r1", "faction": "red", "before": 4, "after": 2}}}}})
	s.at(3000, resolvedType, map[string]any{"round": 3,
		"engagements": []any{map[string]any{"at": cell("a", 3, 0), "elements": []any{
			map[string]any{"id": "b1", "faction": "blue", "before": 1, "after": 0},
			map[string]any{"id": "r1", "faction": "red", "before": 2, "after": 1}}}},
		"losses":   []any{map[string]any{"id": "b1", "faction": "blue"}},
		"captures": []any{map[string]any{"at": cell("a", 4, 4), "faction": "red", "from": ""}}})
	s.at(3001, observedType, map[string]any{"faction": "red", "round": 3, "own": []any{
		sq("r1", "force", 1, cell("a", 3, 0)), sq("r2", "scout", 1, cell("a", 4, 4))}})
	s.at(3002, observedType, map[string]any{"faction": "blue", "round": 3, "own": []any{}})
	s.at(3003, concludedType, map[string]any{"round": 3, "winner": "red", "reason": "elimination"})
	if s.n.settled.isFired() {
		t.Fatal("settled before the final round's assessments")
	}
	s.at(3012, assessmentType, map[string]any{"faction": "red", "round": 3,
		"objectives": []any{map[string]any{"at": cell("a", 4, 4), "holder": "red", "known": true}}})
	s.at(3013, assessmentType, map[string]any{"faction": "blue", "round": 3})
	if !s.n.settled.isFired() {
		t.Fatal("not settled once each faction's final assessment was in")
	}

	want := []string{
		"skirmish: 3 rounds at 1s",
		"  map         a 5×5, b 3×3",
		"  objectives  a:4,4 · b:1,1",
		"  red         r1 force 4 at a:0,0 · r2 scout 1 at a:0,1 · strength 5",
		"  blue        b1 force 3 at a:4,0 · strength 3",
		"",
		"r0   intelligence blue  sees a:4,4 unheld",
		"r0   command      red   r1 captures a:4,4 · r2 holds",
		"r1   operations   red   moves r1",
		"r1   intelligence red   spots b1 (3) at a:3,0",
		"r1   command      red   r1 engages b1 at a:3,0 (was capturing a:4,4)",
		"r2   operations   red   moves r1 r2",
		"r2   exercise           fight at a:3,0: b1 3→1 · r1 4→2",
		"r3   exercise           fight at a:3,0: b1 1→0 · r1 2→1",
		"r3   exercise     blue  b1 destroyed",
		"r3   exercise     red   captures a:4,4",
		"r3   intelligence red   loses track of b1 · sees a:4,4 held",
	}
	if got := strings.Join(s.lines, "\n"); got != strings.Join(want, "\n") {
		t.Errorf("narration:\n%s\n\nwant:\n%s", got, strings.Join(want, "\n"))
	}

	final := strings.Join(s.n.final(), "\n")
	for _, w := range []string{
		"verdict     red wins by elimination after round 3",
		"objectives  a:4,4 red · b:1,1 unheld",
		"red         r1 1 · r2 1 · strength 2",
		"blue        none · strength 0 · lost b1",
		"events      20 on the stream: 1 exercise.started · 3 exercise.round.resolved · 5 exercise.round.observed · " +
			"5 intelligence.assessment.issued · 2 command.directive.issued · 3 operations.orders.issued · 1 exercise.concluded",
		// observed→assessed: red 0 (8ms), red 1 (10ms), red 3 (11ms), blue 3
		// (11ms); assessed→directed: 10ms and 15ms; directed→ordered, to the
		// next round's first orders after it: 10ms and 15ms.
		"chain p50   observed→assessed 11ms · assessed→directed 15ms · directed→ordered 15ms",
	} {
		if !strings.Contains(final, w) {
			t.Errorf("final conditions lack %q:\n%s", w, final)
		}
	}
}

// An event of another exercise is not narrated, and not counted.
func TestNarratorIgnoresOtherExercises(t *testing.T) {
	s := newScript(t)
	body, _ := json.Marshal(map[string]any{"exercise": "another", "round": 1})
	if err := s.n.handle(eventOf(resolvedType, body, s.t0)); err != nil {
		t.Fatal(err)
	}
	if len(s.lines) != 0 || s.n.counts[resolvedType] != 0 {
		t.Errorf("narrated %v, counted %v", s.lines, s.n.counts)
	}
}
