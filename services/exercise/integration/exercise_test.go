//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	libconfig "github.com/standards-lab/go-core/config"
	"github.com/standards-lab/go-web-sdk/webtest"

	"github.com/JaimeStill/spike-messaging/core/event"
	"github.com/JaimeStill/spike-messaging/core/reactor"
	"github.com/JaimeStill/spike-messaging/messaging"
	"github.com/JaimeStill/spike-messaging/messaging/nats"
	"github.com/JaimeStill/spike-messaging/services/exercise/integration"
)

// The API's view of an exercise and a round, as far as the tests read them.
type view struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Round   int    `json:"round"`
	Verdict *struct {
		Over   bool   `json:"over"`
		Winner string `json:"winner"`
		Reason string `json:"reason"`
	} `json:"verdict"`
	Rules struct {
		CaptureRounds int            `json:"capture_rounds"`
		Sight         map[string]int `json:"sight"`
	} `json:"rules"`
}

type round struct {
	Round      int             `json:"round"`
	Resolution json.RawMessage `json:"resolution"`
}

// positions is the view's state as far as where each element stands.
type positions struct {
	State struct {
		Elements []struct {
			ID string `json:"id"`
			At struct {
				X int `json:"x"`
				Y int `json:"y"`
			} `json:"at"`
		} `json:"elements"`
	} `json:"state"`
}

// idle is a create body for two factions standing apart in one 6×6 sector,
// neither on its objective nor in sight of the other, so with no orders
// nothing happens until the round limit.
func idle(limit int, interval string) map[string]any {
	at := func(x, y int) map[string]any { return map[string]any{"sector": "a", "x": x, "y": y} }
	return map[string]any{
		"name":     "idle",
		"map":      map[string]any{"sectors": []any{map[string]any{"id": "a", "width": 6, "height": 6, "objectives": []any{map[string]any{"x": 3, "y": 0}}}}},
		"factions": []string{"red", "blue"},
		"elements": []any{
			map[string]any{"id": "r1", "faction": "red", "kind": "squad", "strength": 300, "health": []int{100, 100, 100}, "status": "ready", "at": at(0, 0)},
			map[string]any{"id": "b1", "faction": "blue", "kind": "squad", "strength": 300, "health": []int{100, 100, 100}, "status": "ready", "at": at(5, 5)},
		},
		"round_interval": interval,
		"round_limit":    limit,
	}
}

// watcher collects every event on a service's scratch stream, through a
// broker of its own on the same stream: the view a downstream service has,
// provisioning the stream with the same configuration, as a downstream
// service must.
type watcher struct {
	broker *nats.Broker
	mu     sync.Mutex
	events []event.Event
}

func watch(t *testing.T, s *integration.Service) *watcher {
	t.Helper()
	// Every broker on a stream provisions it, and the last to provision sets
	// its configuration, so the watcher matches the service's: its default
	// MaxAge, and the default deduplication window.
	b, err := nats.New(nats.Config{
		URL:    integration.NATSURL(),
		Stream: s.Stream,
		Prefix: s.Prefix,
		MaxAge: libconfig.Duration(24 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	w := &watcher{broker: b}
	src, err := b.Subscribe(messaging.Subscription{Name: "watch"})
	if err != nil {
		t.Fatal(err)
	}
	r := reactor.New(src, func(_ context.Context, e event.Event) error {
		w.mu.Lock()
		defer w.mu.Unlock()
		w.events = append(w.events, e)
		return nil
	})
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = r.Shutdown(ctx)
		_ = b.Shutdown(ctx)
	})
	return w
}

func (w *watcher) seen() []event.Event {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]event.Event(nil), w.events...)
}

// await polls until ok holds or the deadline passes.
func await(t *testing.T, what string, d time.Duration, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// On the running binary, an exercise created and started over the API,
// with both factions idle, resolves a round every interval on its own
// reactor and ends at its round limit as a draw. Every event reaches the
// stream in the chain's order: the start, then each round's resolution
// before each faction's observation of that round (observations run from
// round 0 to the limit), then the conclusion. The umpire's view and the
// history agree.
func TestExercise_IdleRunsToItsLimitAsADraw(t *testing.T) {
	s := integration.Start(t, integration.Options{})
	c := s.Client()
	w := watch(t, s)
	const limit = 5

	ex := webtest.Decode[view](t, c.Post(t, "/api/exercises", idle(limit, "200ms")), http.StatusCreated)
	if ex.Status != "created" {
		t.Fatalf("created exercise status = %q", ex.Status)
	}
	webtest.Decode[view](t, c.Post(t, "/api/exercises/"+ex.ID+"/start", nil), http.StatusOK)

	want := 1 + 2*(limit+1) + limit + 1
	await(t, "the exercise's events", 15*time.Second, func() bool { return len(w.seen()) >= want })
	time.Sleep(300 * time.Millisecond) // nothing more follows the conclusion
	events := w.seen()
	if len(events) != want {
		t.Fatalf("saw %d events, want %d", len(events), want)
	}
	if events[0].Type != "exercise.started" || events[len(events)-1].Type != "exercise.concluded" {
		t.Errorf("first and last = %s, %s", events[0].Type, events[len(events)-1].Type)
	}
	i, resolved := 0, 0
	for _, e := range events[1 : len(events)-1] {
		var d struct {
			Faction string `json:"faction"`
			Round   int    `json:"round"`
		}
		if err := json.Unmarshal(e.Data, &d); err != nil {
			t.Fatal(err)
		}
		if e.Type == "exercise.round.resolved" {
			// A round's resolution precedes its observations.
			if resolved++; d.Round != resolved || i != 2*resolved {
				t.Errorf("the resolution of round %d came after %d observations, want round %d's before its own", d.Round, i, resolved)
			}
			continue
		}
		if e.Type != "exercise.round.observed" || d.Round != i/2 || d.Faction != []string{"red", "blue"}[i%2] {
			t.Errorf("event %d = %s round %d faction %q, want the observation of round %d by %s",
				i+1, e.Type, d.Round, d.Faction, i/2, []string{"red", "blue"}[i%2])
		}
		i++
	}
	for _, e := range events {
		if e.Subject != ex.ID || e.Source != "/exercise" {
			t.Errorf("event %s: subject %q source %q, want the exercise and /exercise", e.Type, e.Subject, e.Source)
		}
	}
	var concluded struct {
		Round  int    `json:"round"`
		Winner string `json:"winner"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(events[len(events)-1].Data, &concluded); err != nil {
		t.Fatal(err)
	}
	if concluded.Round != limit || concluded.Winner != "" || concluded.Reason != "limit" {
		t.Errorf("concluded = %+v, want a draw at the limit, round %d", concluded, limit)
	}

	final := webtest.Decode[view](t, c.Get(t, "/api/exercises/"+ex.ID), http.StatusOK)
	if final.Status != "concluded" || final.Round != limit || final.Verdict == nil ||
		final.Verdict.Winner != "" || final.Verdict.Reason != "limit" {
		t.Errorf("final view = %+v, want concluded at round %d, a draw by limit", final, limit)
	}
	// The view carries the rules' constants, so a client reads the state by
	// them rather than mirroring them.
	if final.Rules.CaptureRounds != 2 || len(final.Rules.Sight) != 2 ||
		final.Rules.Sight["squad"] != 1 || final.Rules.Sight["scout"] != 2 {
		t.Errorf("final view's rules = %+v, want capture in 2 rounds, sight 1 for a squad and 2 for a scout", final.Rules)
	}
	history := webtest.Decode[[]round](t, c.Get(t, "/api/exercises/"+ex.ID+"/history"), http.StatusOK)
	if len(history) != limit+1 || history[0].Round != 0 || history[limit].Round != limit {
		t.Fatalf("history has %d rounds, want rounds 0 to %d", len(history), limit)
	}
	// Round 0, the start, resolves nothing; every later round carries its
	// resolution.
	for _, r := range history {
		if isNull := string(r.Resolution) == "null"; isNull != (r.Round == 0) {
			t.Errorf("round %d's resolution = %s", r.Round, r.Resolution)
		}
	}
}

// Orders for a round already resolved are skipped: the orders reactor
// handles and acknowledges the delivery rather than redelivering it, so it
// is logged once, and the exercise is unchanged.
func TestExercise_PastRoundOrdersAreSkipped(t *testing.T) {
	s := integration.Start(t, integration.Options{})
	c := s.Client()
	w := watch(t, s)

	ex := webtest.Decode[view](t, c.Post(t, "/api/exercises", idle(1000, "100ms")), http.StatusCreated)
	webtest.Decode[view](t, c.Post(t, "/api/exercises/"+ex.ID+"/start", nil), http.StatusOK)
	await(t, "round 2", 10*time.Second, func() bool {
		return webtest.Decode[view](t, c.Get(t, "/api/exercises/"+ex.ID), http.StatusOK).Round >= 2
	})
	webtest.Decode[view](t, c.Post(t, "/api/exercises/"+ex.ID+"/pause", nil), http.StatusOK)

	data, err := json.Marshal(map[string]any{
		"exercise": ex.ID, "faction": "red", "round": 1,
		"orders": []any{map[string]any{"element": "r1", "steps": []any{map[string]any{"sector": "a", "x": 1, "y": 0}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.NewV7().String()
	if err := w.broker.Publish(t.Context(), event.Event{
		ID: id, Source: "/operations-test", Type: "operations.orders.issued",
		DataContentType: "application/json", Data: data,
	}); err != nil {
		t.Fatal(err)
	}
	await(t, "the skip", 10*time.Second, func() bool {
		for line := range strings.Lines(s.Output()) {
			if strings.Contains(line, "event="+id) && strings.Contains(line, "outcome=handled") {
				return true
			}
		}
		return false
	})
	time.Sleep(time.Second) // a redelivery would log the delivery again
	if n := strings.Count(s.Output(), "event="+id); n != 1 {
		t.Errorf("the delivery of %s was logged %d times, want once: the skip must acknowledge it", id, n)
	}
	state := webtest.Decode[positions](t, c.Get(t, "/api/exercises/"+ex.ID), http.StatusOK)
	for _, e := range state.State.Elements {
		if e.ID == "r1" && (e.At.X != 0 || e.At.Y != 0) {
			t.Errorf("r1 moved to %d,%d on a skipped order", e.At.X, e.At.Y)
		}
	}
}
