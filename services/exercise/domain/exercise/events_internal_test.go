package exercise

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/JaimeStill/spike-messaging/core/event"
	"github.com/JaimeStill/spike-messaging/services/exercise/domain/exercise/rules"
)

var testState = rules.State{
	Map: rules.Map{Sectors: []rules.Sector{{
		ID: "a", Width: 6, Height: 6,
		Objectives: []rules.Point{{X: 3, Y: 0}},
	}}},
	Factions: [2]string{"red", "blue"},
	Elements: []rules.Element{
		{ID: "r1", Faction: "red", Kind: rules.Scout, Strength: 100, Health: []int{100}, Status: rules.StatusReady,
			At: rules.Location{Sector: "a", Point: rules.Point{X: 1, Y: 0}}},
		{ID: "b1", Faction: "blue", Kind: rules.Squad, Strength: 150, Health: []int{100, 50}, Status: rules.StatusReady,
			At: rules.Location{Sector: "a", Point: rules.Point{X: 5, Y: 5}}},
	},
	Holders:  map[string]string{},
	Progress: map[string]rules.Progress{},
}

// decode returns the data of e as a T.
func decode[T any](t *testing.T, e event.Event) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(e.Data, &v); err != nil {
		t.Fatalf("decode %s: %v", e.Type, err)
	}
	return v
}

// Each faction's observation becomes one observed event about the
// exercise, in the factions' order, carrying the observation's lists as
// they are.
func TestRaiseObserved(t *testing.T) {
	obs := rules.Observe(testState, 4)
	q := &event.Queue{}
	raiseObserved(q, "ex-1", obs)
	es := q.Events()
	if len(es) != 2 {
		t.Fatalf("raised %d events, want 2", len(es))
	}
	for i, e := range es {
		if e.Type != "exercise.round.observed" || e.Subject != "ex-1" {
			t.Errorf("event %d = %s about %q", i, e.Type, e.Subject)
		}
		got := decode[ObservedData](t, e)
		want := ObservedData{
			Exercise: "ex-1", Faction: obs[i].Faction, Round: 4,
			Own: obs[i].Own, Contacts: obs[i].Contacts, Objectives: obs[i].Objectives,
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("event %d data = %+v, want %+v", i, got, want)
		}
	}
	// The red scout sees two cells: the objective, but not the blue squad.
	red := decode[ObservedData](t, es[0])
	if red.Faction != "red" || len(red.Own) != 1 || len(red.Contacts) != 0 || len(red.Objectives) != 1 {
		t.Errorf("red's observation = %+v", red)
	}
}

// The started event carries the public settings, the seed among them, the
// terrain without its objectives, and no element.
func TestRaiseStarted(t *testing.T) {
	ex := Exercise{ID: "ex-1", Name: "n", Seed: 42, RoundLimit: 5, Factions: testState.Factions, State: testState, intervalMS: 2000}
	q := &event.Queue{}
	raiseStarted(q, ex)
	es := q.Events()
	if len(es) != 1 || es[0].Type != "exercise.started" || es[0].Subject != "ex-1" {
		t.Fatalf("raised %+v", es)
	}
	got := decode[StartedData](t, es[0])
	want := StartedData{Exercise: "ex-1", Name: "n", Seed: 42, Map: testState.Map.Terrain(), Factions: testState.Factions, RoundIntervalMS: 2000, RoundLimit: 5}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("data = %+v, want %+v", got, want)
	}
	var raw map[string]any
	_ = json.Unmarshal(es[0].Data, &raw)
	if _, ok := raw["elements"]; ok {
		t.Errorf("started carries elements: %s", es[0].Data)
	}
}

func TestRaiseConcluded(t *testing.T) {
	q := &event.Queue{}
	raiseConcluded(q, "ex-1", 3, rules.Verdict{Over: true, Winner: "red", Reason: "objectives"})
	es := q.Events()
	if len(es) != 1 || es[0].Type != "exercise.concluded" || es[0].Subject != "ex-1" {
		t.Fatalf("raised %+v", es)
	}
	if got, want := decode[ConcludedData](t, es[0]), (ConcludedData{Exercise: "ex-1", Round: 3, Winner: "red", Reason: "objectives"}); got != want {
		t.Errorf("data = %+v, want %+v", got, want)
	}
}

// A faction commands its own elements alone: an order for the other
// faction's element, or for an element that does not exist, is dropped.
func TestOwnOrders(t *testing.T) {
	step := []rules.Location{{Sector: "a", Point: rules.Point{X: 2, Y: 0}}}
	got := ownOrders(testState, map[string][]rules.Order{
		"red":   {{Element: "r1", Steps: step}, {Element: "b1", Steps: step}},
		"blue":  {{Element: "r1"}, {Element: "b1"}, {Element: "ghost"}},
		"green": {{Element: "r1"}},
	})
	want := []rules.Order{{Element: "r1", Steps: step}, {Element: "b1"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ownOrders = %+v, want %+v", got, want)
	}
}

// A round's resolution becomes one resolved event about the exercise, its
// lists flattened beside the exercise and the round.
func TestRaiseResolved(t *testing.T) {
	at := rules.Location{Sector: "a", Point: rules.Point{X: 3, Y: 0}}
	res := rules.Resolution{
		Retreats: []rules.Retreat{{
			ID: "b2", Faction: "blue", From: at, To: rules.Location{Sector: "a", Point: rules.Point{X: 3, Y: 1}},
			Before: 100, After: 60, Fallen: 0,
			Pursuers: []rules.Engaged{{ID: "r1", Faction: "red", Before: 400, After: 400}},
		}},
		Engagements: []rules.Engagement{{At: at, Elements: []rules.Engaged{
			{ID: "b1", Faction: "blue", Before: 40, After: 0, Fallen: 1}, {ID: "r1", Faction: "red", Before: 400, After: 370},
		}}},
		Losses:     []rules.Loss{{ID: "b1", Faction: "blue"}},
		Captures:   []rules.Capture{},
		Progress:   []rules.Advance{{At: at, Faction: "red", Rounds: 1}},
		Objectives: []rules.ObjectiveStatus{{At: at}},
	}
	q := &event.Queue{}
	raiseResolved(q, "ex-1", 7, res)
	es := q.Events()
	if len(es) != 1 || es[0].Type != "exercise.round.resolved" || es[0].Subject != "ex-1" {
		t.Fatalf("raised %+v", es)
	}
	want := `{"exercise":"ex-1","round":7,` +
		`"retreats":[{"id":"b2","faction":"blue","from":{"sector":"a","x":3,"y":0},"to":{"sector":"a","x":3,"y":1},` +
		`"before":100,"after":60,"fallen":0,"pursuers":[{"id":"r1","faction":"red","before":400,"after":400,"fallen":0}]}],` +
		`"engagements":[{"at":{"sector":"a","x":3,"y":0},"elements":[` +
		`{"id":"b1","faction":"blue","before":40,"after":0,"fallen":1},{"id":"r1","faction":"red","before":400,"after":370,"fallen":0}]}],` +
		`"losses":[{"id":"b1","faction":"blue"}],"captures":[],` +
		`"progress":[{"at":{"sector":"a","x":3,"y":0},"faction":"red","rounds":1}],` +
		`"objectives":[{"at":{"sector":"a","x":3,"y":0},"holder":""}]}`
	if string(es[0].Data) != want {
		t.Errorf("data = %s\nwant   %s", es[0].Data, want)
	}
}

// Each capture that takes an objective from its holder alerts the loser;
// a capture of an unheld objective alerts no one.
func TestRaiseLost(t *testing.T) {
	a := rules.Location{Sector: "a", Point: rules.Point{X: 3, Y: 0}}
	b := rules.Location{Sector: "b", Point: rules.Point{X: 5, Y: 5}}
	q := &event.Queue{}
	raiseLost(q, "ex-1", 4, []rules.Capture{{At: a, Faction: "red"}, {At: b, Faction: "blue", From: "red"}})
	es := q.Events()
	if len(es) != 1 || es[0].Type != "exercise.objective.lost" || es[0].Subject != "ex-1" {
		t.Fatalf("raised %+v", es)
	}
	want := LostData{Exercise: "ex-1", Faction: "red", Round: 4, At: b, Holder: "blue"}
	if got := decode[LostData](t, es[0]); got != want {
		t.Errorf("data = %+v, want %+v", got, want)
	}
}
