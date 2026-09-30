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
	"github.com/JaimeStill/spike-messaging/services/command/integration"
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

// directives returns the data of every command.directive.issued event
// seen.
func (w *watcher) directives(t *testing.T) []directiveData {
	t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []directiveData
	for _, e := range w.events {
		if e.Type != "command.directive.issued" {
			continue
		}
		var d directiveData
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

type directive struct {
	Element string    `json:"element"`
	Rule    string    `json:"rule"`
	Contact string    `json:"contact"`
	Target  *location `json:"target"`
}

type directiveData struct {
	Exercise   string      `json:"exercise"`
	Faction    string      `json:"faction"`
	Round      int         `json:"round"`
	Sequence   int         `json:"sequence"`
	Directives []directive `json:"directives"`
}

type direction struct {
	Faction   string      `json:"faction"`
	Status    string      `json:"status"`
	Round     int         `json:"round"`
	Decisions []directive `json:"decisions"`
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

// On the running binary, the inputs arrive as events: an assessment that
// reaches command before the start that opens its direction is redelivered
// and decided on once the start is; a round that changes no target issues
// no directive, and one that brings a weak contact within reach sends the
// squad to engage it; the query shows both factions; the conclusion closes
// the exercise; and the service's log shows the traffic it received and
// sent.
func TestCommand_AssessmentsBecomeDirectives(t *testing.T) {
	s := integration.Start(t, integration.Options{})
	c := s.Client()
	w := watch(t, s)
	id := uuid.NewV7().String()
	at := func(x, y int) location { return location{Sector: "a", X: x, Y: y} }

	r1 := map[string]any{"id": "r1", "faction": "red", "kind": "squad", "strength": 400,
		"health": []int{100, 100, 100, 100}, "status": "ready", "at": at(0, 0)}
	assess := func(round int, contacts []any) {
		w.publish(t, "/intelligence", "intelligence.assessment.issued", id, map[string]any{
			"exercise": id, "faction": "red", "revision": round + 1, "round": round,
			"own":        []any{r1},
			"contacts":   contacts,
			"objectives": []any{map[string]any{"at": at(11, 11), "holder": "", "seen": 0, "age": 0}},
			"explored":   []any{at(0, 0), at(1, 0), at(0, 1), at(1, 1)},
		})
	}

	assess(0, []any{})
	time.Sleep(100 * time.Millisecond)
	w.publish(t, "/exercise", "exercise.started", id, map[string]any{
		"exercise": id, "name": "chain",
		"map":      map[string]any{"sectors": []any{map[string]any{"id": "a", "width": 12, "height": 12}}},
		"factions": []string{"red", "blue"}, "round_interval_ms": 1000, "round_limit": 9,
	})
	await(t, "round 0's directive", 10*time.Second, func() bool { return len(w.directives(t)) >= 1 })
	assess(1, []any{})
	assess(2, []any{map[string]any{"id": "b1", "faction": "blue", "kind": "scout", "strength": 100,
		"health": []int{100}, "status": "ready", "at": at(2, 1), "seen": 2, "age": 0}})
	await(t, "round 2's directive", 10*time.Second, func() bool { return len(w.directives(t)) >= 2 })

	got := w.directives(t)
	want := []directive{
		{Element: "r1", Rule: "secure", Target: &location{Sector: "a", X: 11, Y: 11}},
		{Element: "r1", Rule: "engage", Contact: "b1", Target: &location{Sector: "a", X: 2, Y: 1}},
	}
	for i, r := range []int{0, 2} {
		d := got[i]
		if d.Exercise != id || d.Faction != "red" || d.Round != r || d.Sequence != i+1 || len(d.Directives) != 1 ||
			d.Directives[0].Element != want[i].Element || d.Directives[0].Rule != want[i].Rule ||
			d.Directives[0].Contact != want[i].Contact || *d.Directives[0].Target != *want[i].Target {
			t.Errorf("directive %d = %+v, want round %d: %+v", i, d, r, want[i])
		}
	}

	ds := webtest.Decode[[]direction](t, c.Get(t, "/api/command/"+id), http.StatusOK)
	if len(ds) != 2 || ds[0].Faction != "blue" || ds[0].Round != -1 || ds[1].Faction != "red" || ds[1].Round != 2 ||
		ds[1].Status != "open" || len(ds[1].Decisions) != 1 || ds[1].Decisions[0].Rule != "engage" {
		t.Errorf("directions = %+v", ds)
	}

	w.publish(t, "/exercise", "exercise.concluded", id, map[string]any{"exercise": id, "round": 5, "winner": "", "reason": "stopped"})
	await(t, "the directions to close", 10*time.Second, func() bool {
		ds := webtest.Decode[[]direction](t, c.Get(t, "/api/command/"+id), http.StatusOK)
		return ds[0].Status == "closed" && ds[1].Status == "closed"
	})
	webtest.Decode[map[string]any](t, c.Get(t, "/api/command/"+uuid.NewV7().String()), http.StatusNotFound)

	if n := len(w.directives(t)); n != 2 {
		t.Errorf("%d directives issued, want 2: round 1 changed no target", n)
	}
	for _, line := range []string{
		`msg="event consumed" consumer=command-assessed type=intelligence.assessment.issued`,
		"outcome=retried",
		`msg="event published" type=command.directive.issued`,
		`msg="event consumed" consumer=command-concluded type=exercise.concluded`,
	} {
		if !strings.Contains(s.Output(), line) {
			t.Errorf("the service's log lacks %s", line)
		}
	}
}
