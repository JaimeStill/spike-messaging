//go:build integration

package integration_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/standards-lab/go-web-sdk"
	"github.com/JaimeStill/spike-messaging/services/exercise/integration"
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

// The baseline composition as a running binary. The service boots on the
// port the harness chose. The liveness probe answers, and the readiness
// aggregate reports the coordinator under the app's "lifecycle" name. An
// interrupt drains to exit 0. The build points a service fills in add their
// own cases beside this one.
func TestLifecycle_BootProbeDrain(t *testing.T) {
	s := integration.Start(t, integration.Options{})
	c := s.Client()

	live := webtest.Decode[map[string]string](t, c.Get(t, web.HealthPath), http.StatusOK)
	if live["status"] != "ok" {
		t.Errorf("healthz = %v", live)
	}

	ready := webtest.Decode[readiness](t, c.Get(t, web.ReadyPath), http.StatusOK)
	if ready.Status != "ready" {
		t.Errorf("readyz status = %q", ready.Status)
	}
	found := false
	for _, ch := range ready.Checks {
		if ch.Name == "lifecycle" {
			found = ch.Ready
		}
	}
	if !found {
		t.Errorf("readyz checks = %+v, want the lifecycle check ready", ready.Checks)
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
