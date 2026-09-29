//go:build integration

package integration_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/JaimeStill/spike-messaging/services/exercise/integration"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/go-web-sdk/webtest"
)

// The probe bodies, as the SDK writes them.
type readiness struct {
	Status string `json:"status"`
	Checks []struct {
		Name  string `json:"name"`
		Ready bool   `json:"ready"`
	} `json:"checks"`
}

// The composition as a running binary against the compose stack. The
// service boots on the port the harness chose. The liveness probe answers,
// and the readiness aggregate reports the coordinator under the app's
// "lifecycle" name beside the database, the broker, the schema service, and
// the three reactors: the relay, the resolver, and the orders reactor. An
// interrupt drains to exit 0.
func TestLifecycle_BootProbeDrain(t *testing.T) {
	s := integration.Start(t, integration.Options{})
	c := s.Client()

	live := webtest.Decode[map[string]string](t, c.Get(t, web.HealthPath), http.StatusOK)
	if live["status"] != "ok" {
		t.Errorf("healthz = %v", live)
	}

	// Live means every stage started, but a consumer reports ready only once
	// its source binds its consumer, just after its stage starts.
	s.Await(t, "readiness", func() bool {
		resp, err := http.Get(s.URL() + web.ReadyPath)
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	})
	ready := webtest.Decode[readiness](t, c.Get(t, web.ReadyPath), http.StatusOK)
	if ready.Status != "ready" {
		t.Errorf("readyz status = %q", ready.Status)
	}
	checks := map[string]bool{}
	for _, ch := range ready.Checks {
		checks[ch.Name] = ch.Ready
	}
	for _, name := range []string{"lifecycle", "database", "broker", "schema", "relay", "resolve", "orders"} {
		if !checks[name] {
			t.Errorf("readyz checks = %+v, want %s ready", ready.Checks, name)
		}
	}

	if code := s.Stop(t); code != 0 {
		t.Fatalf("exit = %d, want 0:\n%s", code, s.Output())
	}
	if !strings.Contains(s.Output(), "server stopped") {
		t.Errorf("drain not logged:\n%s", s.Output())
	}
}

// Two composition roots start side by side on their own ports; the
// baseline shares nothing, so both reach ready.
func TestLifecycle_TwoInstances(t *testing.T) {
	a := integration.Launch(t, integration.Options{})
	b := integration.Launch(t, integration.Options{})
	a.Ready(t)
	b.Ready(t)
	if a.Addr() == b.Addr() {
		t.Fatalf("both instances on %s", a.Addr())
	}
	if code := a.Stop(t); code != 0 {
		t.Errorf("a exit = %d:\n%s", code, a.Output())
	}
	if code := b.Stop(t); code != 0 {
		t.Errorf("b exit = %d:\n%s", code, b.Output())
	}
}
