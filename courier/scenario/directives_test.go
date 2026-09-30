package scenario_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JaimeStill/spike-messaging/core/event"
	"github.com/JaimeStill/spike-messaging/core/reactor"
	"github.com/JaimeStill/spike-messaging/courier/scenario"
	"github.com/JaimeStill/spike-messaging/messaging"
	"github.com/JaimeStill/spike-messaging/messaging/memory"
)

const exerciseID = "01999f4e-6a3b-7c2d-8e1f-0a1b2c3d4e5f"

func publishJSON(t *testing.T, b messaging.Broker, id, typ string, data any) {
	t.Helper()
	body, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	e := event.Event{ID: id, Source: "/exercise", Type: typ, DataContentType: "application/json", Data: body}
	if err := b.Publish(t.Context(), e); err != nil {
		t.Fatal(err)
	}
}

// On a stream where exercise has started and observed round 0, the
// scenario directs each of the faction's elements to its own nearest
// objective its observation reports, ignoring another exercise and the
// other faction, and narrates the verdict that follows; its release runs
// once.
func TestDirectivesDirectsAndWaits(t *testing.T) {
	b := memory.New()
	at := func(s string, x, y int) map[string]any { return map[string]any{"sector": s, "x": x, "y": y} }
	publishJSON(t, b, "other", "exercise.round.observed", map[string]any{"exercise": "another", "faction": "red"})
	publishJSON(t, b, "s", "exercise.started", map[string]any{"exercise": exerciseID})
	publishJSON(t, b, "o-blue", "exercise.round.observed", map[string]any{
		"exercise": exerciseID, "faction": "blue", "round": 0, "own": []any{map[string]any{"id": "b1", "at": at("a", 0, 0)}},
		"objectives": []any{},
	})
	publishJSON(t, b, "o-red", "exercise.round.observed", map[string]any{
		"exercise": exerciseID, "faction": "red", "round": 0,
		"objectives": []any{
			map[string]any{"at": at("a", 4, 0), "holder": ""},
			map[string]any{"at": at("a", 0, 4), "holder": ""},
			map[string]any{"at": at("b", 0, 0), "holder": "blue"},
		},
		"own": []any{
			map[string]any{"id": "r2", "at": at("a", 3, 0)},
			map[string]any{"id": "r1", "at": at("a", 4, 1)},
			map[string]any{"id": "r3", "at": at("a", 0, 0)},
			map[string]any{"id": "r4", "at": at("a", 0, 0)},
		},
	})

	// exercise's stand-in: it concludes the exercise once a directive
	// arrives, and keeps the directive for the test.
	got := make(chan json.RawMessage, 1)
	src, err := b.Subscribe(messaging.Subscription{Name: "exercise", Types: []string{"command.directive.issued"}})
	if err != nil {
		t.Fatal(err)
	}
	r := reactor.New(src, func(ctx context.Context, e event.Event) error {
		got <- e.Data
		publishJSON(t, b, "c", "exercise.concluded", map[string]any{"exercise": exerciseID, "round": 4, "winner": "red", "reason": "objectives"})
		return nil
	})
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Shutdown(context.Background()) })

	var released atomic.Int32
	joins := func(stream, prefix string, _ time.Duration) (messaging.Broker, func() error, error) {
		if stream != "exercise" || prefix != "exercise" {
			t.Errorf("joined %s/%s, want the services' default stream", stream, prefix)
		}
		return b, func() error { released.Add(1); return nil }, nil
	}
	for _, s := range scenario.Scenarios(scenario.Dependencies{Joins: joins}) {
		if s.Name != "directives" {
			continue
		}
		rep, out := reporter()
		cmd := scenario.Command(s, func() *scenario.Reporter { return rep })
		cmd.SetArgs([]string{"--exercise", exerciseID, "--faction", "red", "--wait", "5s"})
		if err := cmd.ExecuteContext(t.Context()); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		golden(t, "directives-directs-and-waits", out.String())
		if n := released.Load(); n != 1 {
			t.Errorf("released %d times, want once", n)
		}
	}
	var d struct {
		Exercise, Faction string
		Round             int
		Directives        []struct {
			Element string
			Target  *struct{ Sector string }
		}
	}
	if err := json.Unmarshal(<-got, &d); err != nil {
		t.Fatal(err)
	}
	if d.Exercise != exerciseID || d.Faction != "red" || d.Round != 0 || len(d.Directives) != 4 {
		t.Errorf("directive = %+v", d)
	}
}

// A faction whose first observation reports no objective is directed to
// hold.
func TestDirectivesHoldsWithoutKnownObjectives(t *testing.T) {
	b := memory.New()
	publishJSON(t, b, "o-red", "exercise.round.observed", map[string]any{
		"exercise": exerciseID, "faction": "red", "round": 0, "objectives": []any{},
		"own": []any{map[string]any{"id": "r1", "at": map[string]any{"sector": "a", "x": 0, "y": 0}}},
	})
	publishJSON(t, b, "c", "exercise.concluded", map[string]any{"exercise": exerciseID, "round": 4, "reason": "limit"})
	joins := func(string, string, time.Duration) (messaging.Broker, func() error, error) { return b, nil, nil }
	for _, s := range scenario.Scenarios(scenario.Dependencies{Joins: joins}) {
		if s.Name != "directives" {
			continue
		}
		rep, out := reporter()
		cmd := scenario.Command(s, func() *scenario.Reporter { return rep })
		cmd.SetArgs([]string{"--exercise", exerciseID, "--faction", "red", "--wait", "5s"})
		if err := cmd.ExecuteContext(t.Context()); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		golden(t, "directives-holds", out.String())
	}
}

func TestDirectivesValidatesItsFlags(t *testing.T) {
	for _, args := range [][]string{
		{"--faction", "red"},
		{"--exercise", "nope", "--faction", "red"},
		{"--exercise", exerciseID},
		{"--exercise", exerciseID, "--faction", "red", "--wait", "0s"},
	} {
		if _, err := execute(t, "directives", args...); !strings.Contains(err.Error(), "usage") {
			t.Errorf("%v: err = %v, want a usage error", args, err)
		}
	}
}
