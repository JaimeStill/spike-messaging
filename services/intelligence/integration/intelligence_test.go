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
	"github.com/JaimeStill/spike-messaging/services/intelligence/integration"
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

// assessments returns the data of every intelligence.assessment.issued
// event seen.
func (w *watcher) assessments(t *testing.T) []assessmentData {
	t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []assessmentData
	for _, e := range w.events {
		if e.Type != "intelligence.assessment.issued" {
			continue
		}
		var d assessmentData
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

type contact struct {
	ID   string `json:"id"`
	Seen int    `json:"seen"`
	Age  int    `json:"age"`
}

type assessmentData struct {
	Exercise   string     `json:"exercise"`
	Faction    string     `json:"faction"`
	Round      int        `json:"round"`
	Contacts   []contact  `json:"contacts"`
	Objectives []struct{} `json:"objectives"`
	Explored   []location `json:"explored"`
}

type assessment struct {
	Faction  string    `json:"faction"`
	Status   string    `json:"status"`
	Round    int       `json:"round"`
	Contacts []contact `json:"contacts"`
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

// On the running binary, the inputs arrive as events: a round's observation
// that reaches intelligence before the start that opens its assessment is
// redelivered and handled once the start is; a contact seen in one round
// ages in the rounds after it and drops once it has gone unseen for more
// than contact_rounds, 3 by default; the query shows both factions; and
// the conclusion closes the exercise.
func TestIntelligence_ObservationsBecomeAssessments(t *testing.T) {
	s := integration.Start(t, integration.Options{})
	c := s.Client()
	w := watch(t, s)
	id := uuid.NewV7().String()
	at := func(x, y int) location { return location{Sector: "a", X: x, Y: y} }

	observe := func(round int, contacts []any) {
		w.publish(t, "/exercise", "exercise.round.observed", id, map[string]any{
			"exercise": id, "faction": "red", "round": round,
			"own":      []any{map[string]any{"id": "r1", "faction": "red", "kind": "scout", "strength": 100, "health": []int{100}, "status": "ready", "at": at(0, 0)}},
			"contacts": contacts, "objectives": []any{},
		})
	}
	seen := []any{map[string]any{"id": "b1", "faction": "blue", "kind": "squad", "strength": 100, "health": []int{100}, "status": "ready", "at": at(10, 10)}}

	observe(0, seen)
	time.Sleep(100 * time.Millisecond)
	w.publish(t, "/exercise", "exercise.started", id, map[string]any{
		"exercise": id, "name": "chain",
		"map":      map[string]any{"sectors": []any{map[string]any{"id": "a", "width": 13, "height": 13, "objectives": []any{}}}},
		"factions": []string{"red", "blue"}, "round_interval_ms": 1000, "round_limit": 9,
	})
	await(t, "round 0's assessment", 10*time.Second, func() bool { return len(w.assessments(t)) >= 1 })
	for r := 1; r <= 4; r++ {
		observe(r, []any{})
	}
	await(t, "round 4's assessment", 10*time.Second, func() bool { return len(w.assessments(t)) >= 5 })

	got := w.assessments(t)
	for r, a := range got[:5] {
		if a.Exercise != id || a.Faction != "red" || a.Round != r || len(a.Objectives) != 0 || len(a.Explored) != 9 {
			t.Errorf("assessment %d = %+v", r, a)
		}
		switch {
		case r < 4 && (len(a.Contacts) != 1 || a.Contacts[0].ID != "b1" || a.Contacts[0].Seen != 0 || a.Contacts[0].Age != r):
			t.Errorf("round %d contacts = %+v, want b1 seen in 0, aged %d", r, a.Contacts, r)
		case r == 4 && len(a.Contacts) != 0:
			t.Errorf("round 4 contacts = %+v, want b1 dropped", a.Contacts)
		}
	}

	as := webtest.Decode[[]assessment](t, c.Get(t, "/api/intelligence/"+id), http.StatusOK)
	if len(as) != 2 || as[0].Faction != "blue" || as[0].Round != -1 || as[1].Faction != "red" || as[1].Round != 4 || as[1].Status != "open" {
		t.Errorf("assessments = %+v", as)
	}

	w.publish(t, "/exercise", "exercise.concluded", id, map[string]any{"exercise": id, "round": 5, "winner": "", "reason": "stopped"})
	await(t, "the assessments to close", 10*time.Second, func() bool {
		as := webtest.Decode[[]assessment](t, c.Get(t, "/api/intelligence/"+id), http.StatusOK)
		return as[0].Status == "closed" && as[1].Status == "closed"
	})
	webtest.Decode[map[string]any](t, c.Get(t, "/api/intelligence/"+uuid.NewV7().String()), http.StatusNotFound)
}
