//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
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
	"github.com/JaimeStill/spike-messaging/services/operations/integration"
)

// watcher publishes to a service's scratch stream and collects every event
// on it, through a broker of its own on the same stream, provisioned as the
// service provisions it: the view the other exercise services have.
type watcher struct {
	broker *nats.Broker
	mu     sync.Mutex
	events []event.Event
}

func watch(t *testing.T, s *integration.Service) *watcher {
	t.Helper()
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

// publish publishes an event of typ with data, as the service that owns
// typ would.
func (w *watcher) publish(t *testing.T, source, typ, subject string, data any) {
	t.Helper()
	body, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	e := event.Event{
		ID: uuid.NewV7().String(), Source: source, Type: typ, Subject: subject,
		DataContentType: "application/json", Data: body,
	}
	if err := w.broker.Publish(t.Context(), e); err != nil {
		t.Fatal(err)
	}
}

// orders returns the data of every operations.orders.issued event seen.
func (w *watcher) orders(t *testing.T) []ordersData {
	t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []ordersData
	for _, e := range w.events {
		if e.Type != "operations.orders.issued" {
			continue
		}
		var d ordersData
		if err := json.Unmarshal(e.Data, &d); err != nil {
			t.Fatal(err)
		}
		out = append(out, d)
	}
	return out
}

type location struct {
	Sector string `json:"sector"`
	X      int    `json:"x"`
	Y      int    `json:"y"`
}

type ordersData struct {
	Exercise string `json:"exercise"`
	Faction  string `json:"faction"`
	Round    int    `json:"round"`
	Orders   []struct {
		Element string     `json:"element"`
		Steps   []location `json:"steps"`
	} `json:"orders"`
}

type operation struct {
	Faction   string              `json:"faction"`
	Status    string              `json:"status"`
	LastRound int                 `json:"last_round"`
	Targets   map[string]location `json:"targets"`
	Rules     map[string]string   `json:"rules"`
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

// On the running binary, the chain's inputs arrive as events: a round's
// observation that reaches operations before the start that opens its
// operation is redelivered and handled once the start is; the observation
// issues the next round's orders, none yet; a directive re-issues them
// along a path to its target; and the conclusion closes the exercise.
func TestOperations_DirectivesBecomeOrders(t *testing.T) {
	s := integration.Start(t, integration.Options{})
	c := s.Client()
	w := watch(t, s)
	id := uuid.NewV7().String()
	at := func(x, y int) location { return location{Sector: "a", X: x, Y: y} }

	w.publish(t, "/exercise", "exercise.round.observed", id, map[string]any{
		"exercise": id, "faction": "red", "round": 0,
		"own":      []any{map[string]any{"id": "r1", "faction": "red", "kind": "scout", "strength": 100, "health": []int{100}, "status": "ready", "at": at(0, 0)}},
		"contacts": []any{}, "objectives": []any{},
	})
	time.Sleep(100 * time.Millisecond)
	w.publish(t, "/exercise", "exercise.started", id, map[string]any{
		"exercise": id, "name": "chain",
		"map":      map[string]any{"sectors": []any{map[string]any{"id": "a", "width": 4, "height": 1, "objectives": []any{map[string]any{"x": 3, "y": 0}}}}},
		"factions": []string{"red", "blue"}, "round_interval_ms": 1000, "round_limit": 5,
	})
	await(t, "round 1's orders", 10*time.Second, func() bool { return len(w.orders(t)) >= 1 })

	target := at(3, 0)
	w.publish(t, "/command", "command.directive.issued", id, map[string]any{
		"exercise": id, "faction": "red", "round": 0, "sequence": 1,
		"directives": []any{map[string]any{"element": "r1", "rule": "secure", "contact": nil, "target": target}},
	})
	await(t, "round 1's orders again", 10*time.Second, func() bool { return len(w.orders(t)) >= 2 })

	got := w.orders(t)
	if got[0].Round != 1 || len(got[0].Orders) != 0 {
		t.Errorf("first orders = %+v, want round 1's, empty", got[0])
	}
	if got[1].Round != 1 || len(got[1].Orders) != 1 || len(got[1].Orders[0].Steps) != 2 || got[1].Orders[0].Steps[1] != at(2, 0) {
		t.Errorf("second orders = %+v, want r1's two steps toward 3,0", got[1])
	}

	ops := webtest.Decode[[]operation](t, c.Get(t, "/api/operations/"+id), http.StatusOK)
	if len(ops) != 2 || ops[1].Faction != "red" || ops[1].LastRound != 0 || ops[1].Targets["r1"] != target || ops[1].Rules["r1"] != "secure" {
		t.Errorf("operations = %+v", ops)
	}

	w.publish(t, "/exercise", "exercise.concluded", id, map[string]any{"exercise": id, "round": 1, "winner": "", "reason": "stopped"})
	await(t, "the operations to close", 10*time.Second, func() bool {
		ops := webtest.Decode[[]operation](t, c.Get(t, "/api/operations/"+id), http.StatusOK)
		return ops[0].Status == "closed" && ops[1].Status == "closed"
	})
	webtest.Decode[map[string]any](t, c.Get(t, "/api/operations/"+uuid.NewV7().String()), http.StatusNotFound)
}
