package app_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/standards-lab/go-web-sdk"
	"github.com/JaimeStill/spike-messaging/services/exercise/internal/app"
	"github.com/JaimeStill/spike-messaging/services/exercise/internal/config/configtest"
	mw "github.com/standards-lab/go-web-sdk/middleware"
)

// failsafe bounds every wait for an event that should occur, so a broken
// composition fails the test instead of hanging it.
const failsafe = 2 * time.Second

// syncBuffer serializes writes so the app's logging goroutines and the
// test's reads stay race-free.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// waitForReady polls the log for the coordinator's ready record and returns
// the address the server bound.
func waitForReady(t *testing.T, buf *syncBuffer) string {
	t.Helper()
	deadline := time.Now().Add(failsafe)
	for time.Now().Before(deadline) {
		if out := buf.String(); strings.Contains(out, "server ready") {
			_, after, ok := strings.Cut(out, "addr=")
			if !ok {
				t.Fatalf("ready record carries no addr: %q", out)
			}
			return strings.Fields(after)[0]
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for the server to become ready; log: %q", buf.String())
	return ""
}

// client disables keep-alives so no idle connection outlives its request and
// delays the server's drain.
var client = &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}

// get returns the status code, response header, and body of a GET against
// the running app.
func get(t *testing.T, addr, path string) (int, http.Header, string) {
	t.Helper()
	resp, err := client.Get(fmt.Sprintf("http://%s%s", addr, path))
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s body: %v", path, err)
	}
	return resp.StatusCode, resp.Header, string(body)
}

// The baseline composition end to end. New assembles the process from the
// package's build points. Run serves the probes, and the readiness
// aggregate reports the coordinator under the app's "lifecycle" name. The
// request logger from the middleware stack records the traffic, and a
// cancel drains to exit 0.
//
// The full serve-and-drain pass exists because the baseline's infrastructure
// is inert: no subsystem opens a connection, so startup always succeeds.
// When the first subsystem with a lifecycle arrives, this test gives way to
// startup-contract tests — hermetic runs against a closed port asserting Run
// exits 1, the startup failure names the subsystem, and the server never
// reports ready.
func TestRun_ServesProbesThenDrains(t *testing.T) {
	buf := &syncBuffer{}
	a, err := app.New(configtest.Config(t), buf)
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() { done <- a.Run(ctx) }()

	addr := waitForReady(t, buf)

	code, header, _ := get(t, addr, web.HealthPath)
	if code != http.StatusOK {
		t.Errorf("GET %s = %d, want 200", web.HealthPath, code)
	}
	if header.Get(mw.RequestIDHeader) == "" {
		t.Errorf("GET %s carries no %s response header; RequestID is not wired", web.HealthPath, mw.RequestIDHeader)
	}

	code, _, body := get(t, addr, web.ReadyPath)
	if code != http.StatusOK {
		t.Errorf("GET %s = %d, want 200", web.ReadyPath, code)
	}
	if !strings.Contains(body, `"name":"lifecycle"`) {
		t.Errorf("readiness body = %q, want the coordinator under the \"lifecycle\" name", body)
	}

	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("Run = %d, want 0", code)
		}
	case <-time.After(failsafe):
		t.Fatal("timed out waiting for Run to return")
	}

	out := buf.String()
	if !strings.Contains(out, "server stopped") {
		t.Error("log carries no stop record after the drain")
	}
	if !strings.Contains(out, "url.path="+web.HealthPath) {
		t.Error("log has no probe request record; the middleware stack is not wired")
	}
}

// A second Run cannot exist: the coordinator is single-use, and the exit
// path reports rather than panics only for lifecycle errors — a re-run is a
// programming error and propagates go-core's panic.
func TestRun_TwicePanics(t *testing.T) {
	buf := &syncBuffer{}
	a, err := app.New(configtest.Config(t), buf)
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if code := a.Run(ctx); code != 0 {
		t.Fatalf("first Run = %d, want 0", code)
	}

	defer func() {
		if recover() == nil {
			t.Error("a second Run did not panic")
		}
	}()
	_ = a.Run(ctx)
}
