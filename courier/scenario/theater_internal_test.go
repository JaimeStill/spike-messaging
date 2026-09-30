package scenario

import (
	"context"
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
	// The narration's step has begun, as the theater's second step begins it.
	s.n.begin()
	// The observer's view, as exercise's API tells it at the start: the seed,
	// the rules, and the objectives in a:4,4 and b:1,1.
	s.n.read = func(context.Context) (exerciseView, error) {
		return viewOf(t, rulesBlock()), nil
	}
	s.n.note = func(format string, args ...any) { s.lines = append(s.lines, fmt.Sprintf(format, args...)) }
	return s
}

// rulesBlock is the rules block of exercise's view: a capture takes two
// rounds, and a squad sees one cell, a scout two.
func rulesBlock() map[string]any {
	return map[string]any{"capture_rounds": 2, "sight": map[string]int{"squad": 1, "scout": 2}}
}

// viewOf returns the observer's view the script's narrator reads: the seed
// 42, the given rules, and the objectives in a:4,4 and b:1,1.
func viewOf(t *testing.T, rules map[string]any) exerciseView {
	t.Helper()
	return viaJSON[exerciseView](t, map[string]any{"seed": 42, "rules": rules, "state": map[string]any{"map": map[string]any{"sectors": []any{
		map[string]any{"id": "a", "objectives": []any{map[string]any{"x": 4, "y": 4}}},
		map[string]any{"id": "b", "objectives": []any{map[string]any{"x": 1, "y": 1}}},
	}}}})
}

// The narrator refuses a view of exercise's that lacks the rules, rather
// than narrate the captures under rules it does not know.
func TestNarratorRefusesAViewWithoutRules(t *testing.T) {
	for _, tc := range []struct {
		name  string
		rules map[string]any
		want  string
	}{
		{"no rules", nil, "the rules give no capture_rounds\nthe rules give no sight"},
		{"no capture rounds", map[string]any{"sight": map[string]int{"squad": 1}}, "the rules give no capture_rounds"},
		{"no sight", map[string]any{"capture_rounds": 2}, "the rules give no sight"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := newNarrator(theaterID)
			n.read = func(context.Context) (exerciseView, error) { return viewOf(t, tc.rules), nil }
			body, err := json.Marshal(map[string]any{"exercise": theaterID, "name": "skirmish", "factions": []string{"red", "blue"}})
			if err != nil {
				t.Fatal(err)
			}
			err = n.handle(t.Context(), eventOf(startedType, body, time.Now()))
			if err == nil || !strings.HasSuffix(err.Error(), tc.want) {
				t.Fatalf("err = %v, want one ending %q", err, tc.want)
			}
			if n.world.setup != nil {
				t.Error("the narrator took the start of an exercise whose rules it refused")
			}
		})
	}
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
	if err := s.n.handle(s.t.Context(), e); err != nil {
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

// The narrator reads the seed and the objectives from exercise's API at the
// start, tells the initial conditions once both round-0 observations are in,
// and names an objective's cell objective:x,y throughout. It then tells each round as a block once an event of a
// later round arrives: what the observer recorded (fights, with operators
// down or squads destroyed, retreats pursued or not, and each faction's
// captures, from a holder or not, objectives being taken, objectives lost,
// and squads regrouping), then what each faction knows (sightings, contacts
// lost, objectives seen held or unheld, a loss learned from the alert),
// decides (a changed directive of each kind, one squad to a line), and
// orders (a new set of moving squads, or none), in the round it issued
// them. It tells nothing for an event that changes nothing (an engagement
// pursuing its contact, or orders that exercise refuses as late), a round
// that changes nothing as such, and a change for a round already told as
// late, in the next block. Its final conditions give the verdict, the
// holders, the survivors and losses, the events by type, the median
// latency of each hop, and the check to run.
func TestNarratorTellsWhatChanges(t *testing.T) {
	s := newScript(t)
	s.at(0, startedType, map[string]any{
		"name": "skirmish", "factions": []string{"red", "blue"}, "round_interval_ms": 1000, "round_limit": 12,
		"map": map[string]any{"sectors": []any{
			map[string]any{"id": "a", "width": 5, "height": 5, "objectives": []any{}},
			map[string]any{"id": "b", "width": 3, "height": 3, "objectives": []any{}},
		}},
	})
	// An assessment arriving before the initial conditions waits for them.
	s.at(1, assessmentType, map[string]any{"faction": "blue", "round": 0,
		"objectives": []any{map[string]any{"at": cell("a", 4, 4), "holder": ""}}})
	s.at(2, observedType, map[string]any{"faction": "red", "round": 0, "own": []any{
		sq("r1", "squad", cell("a", 0, 0), 100, 100, 100, 100), sq("r2", "scout", cell("a", 0, 1), 100)}})
	s.at(3, observedType, map[string]any{"faction": "blue", "round": 0, "own": []any{
		sq("b1", "squad", cell("a", 4, 0), 100, 100, 100)}})
	s.at(10, assessmentType, map[string]any{"faction": "red", "round": 0})
	s.at(20, directiveType, map[string]any{"faction": "red", "round": 0, "directives": []any{
		map[string]any{"element": "r1", "rule": "secure", "target": cell("a", 4, 4)},
		map[string]any{"element": "r2", "rule": "hold", "target": nil}}})
	s.at(30, ordersType, map[string]any{"faction": "red", "round": 1, "orders": []any{
		map[string]any{"element": "r1", "steps": []any{cell("a", 1, 0)}}}})
	// Round 0's observations are in: the initial conditions are told.
	if len(s.lines) == 0 {
		t.Fatal("narrated nothing once round 0's observations were in")
	}

	s.at(1000, resolvedType, map[string]any{"round": 1, "retreats": []any{}, "engagements": []any{}, "captures": []any{}, "losses": []any{}, "progress": []any{}})
	s.at(1001, observedType, map[string]any{"faction": "red", "round": 1, "own": []any{
		sq("r1", "squad", cell("a", 1, 0), 100, 100, 100, 100), sq("r2", "scout", cell("a", 0, 1), 100)}})
	s.at(1002, observedType, map[string]any{"faction": "blue", "round": 1, "own": []any{
		sq("b1", "squad", cell("a", 3, 0), 100, 100, 100)}})
	s.at(1011, assessmentType, map[string]any{"faction": "red", "round": 1,
		"contacts": []any{map[string]any{"id": "b1", "strength": 300, "at": cell("a", 3, 0), "seen": 1, "age": 0}}})
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
	// r1 holds its fight to fire on a retreat; r2 still reinforces.
	s.at(2032, directiveType, map[string]any{"faction": "red", "round": 2, "directives": []any{
		map[string]any{"element": "r1", "rule": "pursue", "contact": "b1", "target": cell("a", 3, 0)},
		map[string]any{"element": "r2", "rule": "reinforce", "contact": "b1", "target": cell("a", 3, 0)}}})
	s.at(3000, resolvedType, map[string]any{"round": 3, "engagements": []any{}, "losses": []any{}, "captures": []any{},
		"retreats": []any{map[string]any{"id": "b1", "faction": "blue", "from": cell("a", 3, 0), "to": cell("a", 4, 4),
			"before": 190, "after": 100, "fallen": 1, "pursuers": []any{
				map[string]any{"id": "r1", "faction": "red", "before": 400, "after": 340, "fallen": 0},
				map[string]any{"id": "r3", "faction": "red", "before": 200, "after": 150, "fallen": 1}}}},
		"progress": []any{map[string]any{"at": cell("a", 4, 4), "faction": "red", "rounds": 1}}})
	// r2 stands on a:4,4 to take it, and r1 holds its fight: none of red's
	// squads moves.
	s.at(3030, ordersType, map[string]any{"faction": "red", "round": 4, "orders": []any{
		map[string]any{"element": "r1", "steps": []any{}}, map[string]any{"element": "r2", "steps": []any{}}}})
	// r2 looks for objectives.
	s.at(3012, directiveType, map[string]any{"faction": "red", "round": 3, "directives": []any{
		map[string]any{"element": "r1", "rule": "pursue", "contact": "b1", "target": cell("a", 3, 0)},
		map[string]any{"element": "r2", "rule": "search", "target": cell("b", 0, 0)}}})
	// blue's directive for round 2 arrives once round 3 has resolved and
	// round 2 is narrated: it is told late, in round 3's block.
	s.at(3001, directiveType, map[string]any{"faction": "blue", "round": 2, "directives": []any{
		map[string]any{"element": "b1", "rule": "hold", "target": nil}}})
	s.at(4000, resolvedType, map[string]any{"round": 4, "retreats": []any{}, "progress": []any{},
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
	s.at(4012, assessmentType, map[string]any{"faction": "red", "round": 4,
		"objectives": []any{map[string]any{"at": cell("a", 4, 4), "holder": "red"}}})
	s.at(4013, assessmentType, map[string]any{"faction": "blue", "round": 4,
		"objectives": []any{map[string]any{"at": cell("a", 4, 4), "holder": "red", "seen": 4}}})
	// intelligence revises blue's round-4 assessment for the alert: the
	// narration tells what the revision changed, and observed -> assessed
	// still measures the round's first assessment.
	s.at(4014, assessmentType, map[string]any{"faction": "blue", "round": 4,
		"objectives": []any{map[string]any{"at": cell("a", 4, 4), "holder": "red", "seen": 4},
			map[string]any{"at": cell("b", 1, 1), "holder": "red", "seen": 4}}})
	s.at(4022, directiveType, map[string]any{"faction": "red", "round": 4, "directives": []any{
		map[string]any{"element": "r1", "rule": "pursue", "contact": "b1", "target": cell("a", 3, 0)},
		map[string]any{"element": "r2", "rule": "rescout", "target": cell("b", 1, 1)}}})
	// Round 5 changes nothing.
	s.at(5000, resolvedType, map[string]any{"round": 5, "retreats": []any{}, "engagements": []any{}, "losses": []any{}, "captures": []any{}, "progress": []any{}})
	s.at(5001, observedType, map[string]any{"faction": "red", "round": 5, "own": []any{
		sq("r1", "squad", cell("a", 3, 0), 100, 100, 100, 10), sq("r2", "scout", cell("a", 4, 4), 100)}})
	s.at(5002, observedType, map[string]any{"faction": "blue", "round": 5, "own": []any{}})
	s.at(5003, concludedType, map[string]any{"round": 5, "winner": "red", "reason": "elimination"})
	if s.n.settled.isFired() {
		t.Fatal("settled before the final round's assessments")
	}
	s.at(5012, assessmentType, map[string]any{"faction": "red", "round": 5,
		"objectives": []any{map[string]any{"at": cell("a", 4, 4), "holder": "red"}}})
	s.at(5013, assessmentType, map[string]any{"faction": "blue", "round": 5,
		"objectives": []any{map[string]any{"at": cell("a", 4, 4), "holder": "red", "seen": 4},
			map[string]any{"at": cell("b", 1, 1), "holder": "red", "seen": 4}}})
	if !s.n.settled.isFired() {
		t.Fatal("not settled once each faction's final assessment was in")
	}

	want := []string{
		"skirmish: 12 rounds at 1s",
		"  map size    a 5x5, b 3x3",
		"  seed        42",
		"  objectives  hidden from both factions",
		"    objective:4,4",
		"    objective:1,1",
		"  red",
		"    r1 squad 4x100 @ a:0,0",
		"    r2 scout 1x100 @ a:0,1",
		"  blue",
		"    b1 squad 3x100 @ a:4,0",
		"",
		"round 0",
		"  red",
		"    decides  r1 capture -> objective:4,4",
		"             r2 hold",
		"    orders   move r1",
		"  blue",
		"    knows    objective:4,4 unheld",
		"",
		"round 1",
		"  red",
		"    knows    spotted b1 300 @ a:3,0",
		"    decides  r1 engage -> a:3,0 b1",
		"    orders   move r1 r2",
		"  blue",
		"    decides  b1 fight @ a:3,0 r1",
		"",
		// Round 2 lists the fight before the retreat and, with the
		// observer, r2 regrouping, which red's observation tells. The
		// decides row lists one squad to a line, aligned.
		"round 2",
		"  observer",
		"    fight    a:3,0  b1 300->190 (1 down)  r1 400->310",
		"    retreat  r2 a:0,1 -> a:0,0 unpursued",
		"    red      r2 regroups, sits out 3",
		"  red",
		"    decides  r1 pursue    @ a:3,0  b1",
		"             r2 reinforce -> a:3,0",
		"    orders   move r2 · pursue r1",
		"  blue",
		"    decides  b1 retreat -> objective:4,4",
		"    orders   retreat b1",
		"",
		"round 3",
		"  observer",
		"    retreat  b1 a:3,0 -> objective:4,4 pursued  b1 190->100 (1 down), r1 400->340, r3 200->150 (1 down)",
		"    red      takes objective:4,4 1/2",
		"  red",
		"    decides  r2 search -> b:0,0",
		"    orders   all hold",
		// blue's decision for round 2 arrived after its block.
		"  late     2 blue decides b1 hold",
		"",
		"round 4",
		"  observer",
		"    fight    objective:4,4  b1 100->0 destroyed  r2 100->60",
		"    red      captures objective:4,4 · objective:1,1 from blue",
		"    blue     loses objective:1,1 to red",
		"  red",
		"    knows    lost b1 · objective:4,4 held by red",
		"    decides  r2 rescout -> objective:1,1",
		"  blue",
		"    knows    objective:4,4 held by red · objective:1,1 lost to red",
		"",
		"round 5  no change",
	}
	// The last round's block waits for the narration to finish.
	s.n.finish()
	if got := strings.Join(s.lines, "\n"); got != strings.Join(want, "\n") {
		t.Errorf("narration:\n%s\n\nwant:\n%s", got, strings.Join(want, "\n"))
	}

	// observed -> assessed: red 0 (8ms), red 1 (10ms), red 4 (11ms), blue 4
	// (11ms, its first assessment, not the revision), red 5 and blue 5
	// (11ms); assessed -> directed: 10ms, 15ms, and red 4's 10ms; directed
	// -> ordered, to the next round's first orders after it, medians at
	// 18ms.
	want = []string{
		"verdict  red wins by elimination after round 5",
		"objectives",
		"  objective:4,4  red",
		"  objective:1,1  red",
		"red  strength 410",
		"  r1 squad 100,100,100,10",
		"  r2 scout 1x100",
		"blue  strength 0, lost b1",
		"events  43",
		"  exercise.started                 1",
		"  exercise.round.resolved          5",
		"  exercise.objective.lost          1",
		"  exercise.round.observed         11",
		"  intelligence.assessment.issued   8",
		"  command.directive.issued         9",
		"  operations.orders.issued         7",
		"  exercise.concluded               1",
		"chain p50",
		"  observed -> assessed  11ms",
		"  assessed -> directed  10ms (rounds with a directive)",
		"  directed -> ordered   18ms",
		"check  mise run demo-theater-check " + theaterID,
	}
	if got := strings.Join(s.n.conditions(), "\n"); got != strings.Join(want, "\n") {
		t.Errorf("final conditions:\n%s\n\nwant:\n%s", got, strings.Join(want, "\n"))
	}
}

// An event of another exercise is not narrated, and not counted.
func TestNarratorIgnoresOtherExercises(t *testing.T) {
	s := newScript(t)
	body, _ := json.Marshal(map[string]any{"exercise": "another", "round": 1})
	if err := s.n.handle(t.Context(), eventOf(resolvedType, body, s.t0)); err != nil {
		t.Fatal(err)
	}
	if len(s.lines) != 0 || s.n.ledger.counts[resolvedType] != 0 {
		t.Errorf("narrated %v, counted %v", s.lines, s.n.ledger.counts)
	}
}

// The narrator settles once the concluded round's assessments are in, and,
// for a faction alerted to an objective lost in that round, a revision of
// its assessment, which intelligence issues after the alert.
func TestNarratorSettlesOnTheFinalRevision(t *testing.T) {
	s := newScript(t)
	s.at(0, startedType, map[string]any{"name": "skirmish", "factions": []string{"red", "blue"}, "round_limit": 1})
	s.at(1, concludedType, map[string]any{"round": 1, "winner": "red", "reason": "limit"})
	s.at(2, lostType, map[string]any{"faction": "blue", "round": 1, "at": cell("b", 1, 1), "holder": "red"})
	s.at(3, assessmentType, map[string]any{"faction": "red", "round": 1, "revision": 1})
	s.at(4, assessmentType, map[string]any{"faction": "blue", "round": 1, "revision": 1})
	// A redelivery of the same assessment is no revision.
	s.at(5, assessmentType, map[string]any{"faction": "blue", "round": 1, "revision": 1})
	if s.n.settled.isFired() {
		t.Fatal("settled before blue's assessment was revised for the alert")
	}
	s.at(6, assessmentType, map[string]any{"faction": "blue", "round": 1, "revision": 2})
	if !s.n.settled.isFired() {
		t.Fatal("not settled once blue's final assessment was revised")
	}
}
