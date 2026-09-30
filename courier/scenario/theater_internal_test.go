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

// sq is an element of kind at a cell, whose operators have the given health.
func sq(id, kind string, at map[string]any, health ...int) map[string]any {
	strength := 0
	for _, h := range health {
		strength += h
	}
	return map[string]any{"id": id, "kind": kind, "strength": strength, "health": health, "status": "ready", "at": at}
}

// The narrator tells the initial conditions once the start, both round-0
// observations, and the first resolution, which tells the objectives, are
// in, and names an objective's cell objective:x,y, in the lines it held as
// in the others. It then tells one line for each event that changes
// something (a sighting, a changed directive, a new set of moving squads in
// the orders in effect for a round, a fight, a loss, a capture, an
// objective seen held, a retreat under fire or unpursued, a squad
// regrouping, a fight with operators down, an objective being taken, an
// objective lost, a revised assessment that learns of the loss) and
// nothing for an event that changes nothing (an engagement pursuing its
// contact, or orders that exercise refuses as late). Its final conditions
// give the verdict, the holders, the survivors and losses, the events by
// type, and the median latency of each hop.
func TestNarratorTellsWhatChanges(t *testing.T) {
	s := newScript(t)
	s.at(0, startedType, map[string]any{
		"name": "skirmish", "factions": []string{"red", "blue"}, "round_interval_ms": 1000, "round_limit": 5, "seed": 42,
		"map": map[string]any{"sectors": []any{
			map[string]any{"id": "a", "width": 5, "height": 5, "objectives": []any{}},
			map[string]any{"id": "b", "width": 3, "height": 3, "objectives": []any{}},
		}},
	})
	// An assessment arriving before the initial conditions waits for them,
	// and is named for the objectives they tell.
	s.at(1, assessmentType, map[string]any{"faction": "blue", "round": 0,
		"objectives": []any{map[string]any{"at": cell("a", 4, 4), "holder": "", "known": true}}})
	s.at(2, observedType, map[string]any{"faction": "red", "round": 0, "own": []any{
		sq("r1", "squad", cell("a", 0, 0), 100, 100, 100, 100), sq("r2", "scout", cell("a", 0, 1), 100)}})
	s.at(3, observedType, map[string]any{"faction": "blue", "round": 0, "own": []any{
		sq("b1", "squad", cell("a", 4, 0), 100, 100, 100)}})
	s.at(10, assessmentType, map[string]any{"faction": "red", "round": 0,
		"objectives": []any{map[string]any{"at": cell("a", 4, 4), "known": false}}})
	s.at(20, directiveType, map[string]any{"faction": "red", "round": 0, "directives": []any{
		map[string]any{"element": "r1", "rule": "secure", "target": cell("a", 4, 4)},
		map[string]any{"element": "r2", "rule": "hold", "target": nil}}})
	s.at(30, ordersType, map[string]any{"faction": "red", "round": 1, "orders": []any{
		map[string]any{"element": "r1", "steps": []any{cell("a", 1, 0)}}}})
	// Round 0's observations are in, but the objectives are not yet told.
	if len(s.lines) != 0 {
		t.Fatalf("narrated before the first resolution: %v", s.lines)
	}

	s.at(1000, resolvedType, map[string]any{"round": 1, "retreats": []any{}, "engagements": []any{}, "captures": []any{}, "losses": []any{}, "progress": []any{},
		"objectives": []any{map[string]any{"at": cell("a", 4, 4), "holder": ""}, map[string]any{"at": cell("b", 1, 1), "holder": ""}}})
	s.at(1001, observedType, map[string]any{"faction": "red", "round": 1, "own": []any{
		sq("r1", "squad", cell("a", 1, 0), 100, 100, 100, 100), sq("r2", "scout", cell("a", 0, 1), 100)}})
	s.at(1002, observedType, map[string]any{"faction": "blue", "round": 1, "own": []any{
		sq("b1", "squad", cell("a", 3, 0), 100, 100, 100)}})
	s.at(1011, assessmentType, map[string]any{"faction": "red", "round": 1,
		"contacts":   []any{map[string]any{"id": "b1", "strength": 300, "at": cell("a", 3, 0), "seen": 1, "age": 0}},
		"objectives": []any{map[string]any{"at": cell("a", 4, 4), "known": false}}})
	s.at(1005, ordersType, map[string]any{"faction": "red", "round": 2, "orders": []any{
		map[string]any{"element": "r1", "steps": []any{cell("a", 2, 0)}}}})
	s.at(1026, directiveType, map[string]any{"faction": "red", "round": 1, "directives": []any{
		map[string]any{"element": "r1", "rule": "engage", "contact": "b1", "target": cell("a", 3, 0)},
		map[string]any{"element": "r2", "rule": "hold", "target": nil}}})
	// b1 already stands in the cell it engages r1 in, so it holds the fight.
	s.at(1027, directiveType, map[string]any{"faction": "blue", "round": 1, "directives": []any{
		map[string]any{"element": "b1", "rule": "engage", "contact": "r1", "target": cell("a", 3, 0)}}})
	s.at(1041, ordersType, map[string]any{"faction": "red", "round": 2, "orders": []any{
		map[string]any{"element": "r1", "steps": []any{cell("a", 2, 0)}},
		map[string]any{"element": "r2", "steps": []any{cell("a", 0, 2)}}}})

	s.at(2000, resolvedType, map[string]any{"round": 2, "captures": []any{}, "losses": []any{}, "progress": []any{},
		"objectives": []any{map[string]any{"at": cell("a", 4, 4), "holder": ""}, map[string]any{"at": cell("b", 1, 1), "holder": ""}},
		// r2 falls back without a shot fired on it.
		"retreats": []any{map[string]any{"id": "r2", "faction": "red", "from": cell("a", 0, 1), "to": cell("a", 0, 0),
			"before": 100, "after": 100, "fallen": 0, "pursuers": []any{}}},
		"engagements": []any{map[string]any{"at": cell("a", 3, 0), "elements": []any{
			map[string]any{"id": "b1", "faction": "blue", "before": 300, "after": 190, "fallen": 1},
			map[string]any{"id": "r1", "faction": "red", "before": 400, "after": 310, "fallen": 0}}}}})
	s.at(2001, observedType, map[string]any{"faction": "blue", "round": 2, "own": []any{
		sq("b1", "squad", cell("a", 3, 0), 100, 90)}})
	// r2 retreated, so it is recovering: narrated once, though the
	// observation is issued again.
	recovering := sq("r2", "scout", cell("a", 0, 0), 100)
	recovering["status"] = "recovering"
	for _, ms := range []int{2002, 2003} {
		s.at(ms, observedType, map[string]any{"faction": "red", "round": 2, "own": []any{
			sq("r1", "squad", cell("a", 3, 0), 100, 100, 100, 10), recovering}})
	}
	// r1 pursues b1 into another cell: the same engagement, not narrated.
	s.at(2010, directiveType, map[string]any{"faction": "red", "round": 2, "directives": []any{
		map[string]any{"element": "r1", "rule": "engage", "contact": "b1", "target": cell("a", 3, 1)},
		map[string]any{"element": "r2", "rule": "reinforce", "contact": "b1", "target": cell("a", 3, 0)}}})
	s.at(2011, directiveType, map[string]any{"faction": "blue", "round": 2, "directives": []any{
		map[string]any{"element": "b1", "rule": "retreat", "contact": "r1", "target": cell("a", 4, 4)}}})
	// Orders for a round already resolved, which exercise refuses, are not
	// narrated.
	s.at(2020, ordersType, map[string]any{"faction": "red", "round": 2, "orders": []any{}})
	s.at(2030, ordersType, map[string]any{"faction": "red", "round": 3, "orders": []any{
		map[string]any{"element": "r1", "steps": []any{}, "pursue": true},
		map[string]any{"element": "r2", "steps": []any{cell("a", 1, 1)}}}})
	s.at(2031, ordersType, map[string]any{"faction": "blue", "round": 3, "orders": []any{
		map[string]any{"element": "b1", "steps": []any{cell("a", 4, 4)}, "retreat": true}}})
	// r1 holds its fight to fire on a retreat, and r2 looks for objectives.
	s.at(2032, directiveType, map[string]any{"faction": "red", "round": 2, "directives": []any{
		map[string]any{"element": "r1", "rule": "pursue", "target": cell("a", 3, 0)},
		map[string]any{"element": "r2", "rule": "search", "target": cell("b", 0, 0)}}})
	s.at(3000, resolvedType, map[string]any{"round": 3, "engagements": []any{}, "losses": []any{}, "captures": []any{},
		"objectives": []any{map[string]any{"at": cell("a", 4, 4), "holder": ""}, map[string]any{"at": cell("b", 1, 1), "holder": "blue"}},
		"retreats": []any{map[string]any{"id": "b1", "faction": "blue", "from": cell("a", 3, 0), "to": cell("a", 4, 4),
			"before": 190, "after": 100, "fallen": 1, "pursuers": []any{
				map[string]any{"id": "r1", "faction": "red", "before": 400, "after": 340, "fallen": 0},
				map[string]any{"id": "r3", "faction": "red", "before": 200, "after": 150, "fallen": 1}}}},
		"progress": []any{map[string]any{"at": cell("a", 4, 4), "faction": "red", "rounds": 1}}})
	s.at(4000, resolvedType, map[string]any{"round": 4, "retreats": []any{}, "progress": []any{},
		"objectives": []any{map[string]any{"at": cell("a", 4, 4), "holder": "red"}, map[string]any{"at": cell("b", 1, 1), "holder": "red"}},
		"engagements": []any{map[string]any{"at": cell("a", 4, 4), "elements": []any{
			map[string]any{"id": "b1", "faction": "blue", "before": 100, "after": 0, "fallen": 1},
			map[string]any{"id": "r2", "faction": "red", "before": 100, "after": 60, "fallen": 0}}}},
		"losses": []any{map[string]any{"id": "b1", "faction": "blue"}},
		"captures": []any{map[string]any{"at": cell("a", 4, 4), "faction": "red", "from": ""},
			map[string]any{"at": cell("b", 1, 1), "faction": "red", "from": "blue"}}})
	// exercise alerts blue that it lost b:1,1, which none of its squads sees.
	s.at(4000, lostType, map[string]any{"faction": "blue", "round": 4, "at": cell("b", 1, 1), "holder": "red"})
	s.at(4001, observedType, map[string]any{"faction": "red", "round": 4, "own": []any{
		sq("r1", "squad", cell("a", 3, 0), 100, 100, 100, 10), sq("r2", "scout", cell("a", 4, 4), 100)}})
	s.at(4002, observedType, map[string]any{"faction": "blue", "round": 4, "own": []any{}})
	s.at(4003, concludedType, map[string]any{"round": 4, "winner": "red", "reason": "elimination"})
	if s.n.settled.isFired() {
		t.Fatal("settled before the final round's assessments")
	}
	s.at(4012, assessmentType, map[string]any{"faction": "red", "round": 4,
		"objectives": []any{map[string]any{"at": cell("a", 4, 4), "holder": "red", "known": true}}})
	s.at(4013, assessmentType, map[string]any{"faction": "blue", "round": 4})
	if !s.n.settled.isFired() {
		t.Fatal("not settled once each faction's final assessment was in")
	}
	// intelligence revises blue's round-4 assessment for the alert: the
	// narration tells what the revision changed, and observed→assessed
	// still measures the round's first assessment.
	s.at(4014, assessmentType, map[string]any{"faction": "blue", "round": 4,
		"objectives": []any{map[string]any{"at": cell("b", 1, 1), "holder": "red", "known": true, "seen": 4}}})
	s.at(4022, directiveType, map[string]any{"faction": "red", "round": 4, "directives": []any{
		map[string]any{"element": "r1", "rule": "pursue", "target": cell("a", 3, 0)},
		map[string]any{"element": "r2", "rule": "rescout", "target": cell("b", 1, 1)}}})

	want := []string{
		"skirmish: 5 rounds at 1s",
		"  map         a 5×5, b 3×3",
		"  objectives  objective:4,4 · objective:1,1 (hidden from both factions)",
		"  seed        42",
		"  red         r1 squad 400 (4 ops) at a:0,0 · r2 scout 100 (1 op) at a:0,1 · strength 500",
		"  blue        b1 squad 300 (3 ops) at a:4,0 · strength 300",
		"",
		"r0   intelligence blue  sees objective:4,4 unheld",
		"r0   command      red   r1 captures objective:4,4 · r2 holds",
		"r1   operations   red   moves r1",
		"r1   intelligence red   spots b1 (300) at a:3,0",
		"r1   command      red   r1 engages b1 at a:3,0 (was capturing objective:4,4)",
		"r1   command      blue  b1 holds the fight at a:3,0",
		"r2   operations   red   moves r1 r2",
		"r2   exercise     red   r2 falls back from a:0,1 to a:0,0 unpursued",
		"r2   exercise           fight at a:3,0: b1 300→190 (1 down) · r1 400→310",
		"r2   exercise     red   r2 regroups, and sits out round 3",
		"r2   command      red   r2 reinforces the fight at a:3,0 (was holding)",
		"r2   command      blue  b1 falls back to objective:4,4 (was holding the fight at a:3,0)",
		"r2   command      red   r1 holds the fight at a:3,0, ready to fire on a retreat (was engaging b1 at a:3,1) · r2 searches toward b:0,0 (was reinforcing the fight at a:3,0)",
		"r3   operations   red   moves r1 (pursue) r2",
		"r3   operations   blue  moves b1 (retreat)",
		"r3   exercise     blue  b1 falls back from a:3,0 to objective:4,4 under fire: 190→100, 1 down; fires back: r1 400→340, r3 200→150 (1 down)",
		"r3   exercise     red   is taking objective:4,4, 1 of 2",
		"r4   exercise           fight at objective:4,4: b1 100→0 (1 down) · r2 100→60",
		"r4   exercise     blue  b1 destroyed",
		"r4   exercise     red   captures objective:4,4",
		"r4   exercise     red   captures objective:1,1 from blue",
		"r4   exercise     blue  loses objective:1,1 to red",
		"r4   intelligence red   loses track of b1 · sees objective:4,4 held",
		"r4   intelligence blue  learns objective:1,1 fell to red",
		"r4   command      red   r2 re-scouts objective:1,1 (was searching toward b:0,0)",
	}
	if got := strings.Join(s.lines, "\n"); got != strings.Join(want, "\n") {
		t.Errorf("narration:\n%s\n\nwant:\n%s", got, strings.Join(want, "\n"))
	}

	final := strings.Join(s.n.final(), "\n")
	for _, w := range []string{
		"verdict     red wins by elimination after round 4",
		"objectives  objective:4,4 red · objective:1,1 red",
		"red         r1 310 · r2 100 · strength 410",
		"blue        none · strength 0 · lost b1",
		"events      35 on the stream: 1 exercise.started · 4 exercise.round.resolved · 1 exercise.objective.lost · " +
			"9 exercise.round.observed · 6 intelligence.assessment.issued · 7 command.directive.issued · " +
			"6 operations.orders.issued · 1 exercise.concluded",
		// observed→assessed: red 0 (8ms), red 1 (10ms), red 4 (11ms), blue 4
		// (11ms, its first assessment, not the revision); assessed→directed:
		// 10ms, 15ms, and red 4's 10ms; directed→ordered, to the next round's
		// first orders after it, medians at 20ms.
		"chain p50   observed→assessed 11ms · assessed→directed 10ms (rounds with a directive) · directed→ordered 20ms",
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
