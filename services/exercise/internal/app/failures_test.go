package app

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// A failure that repeats on every tick is logged at once, then once per
// interval with the count held back, and its recovery once.
func TestFailuresThrottleARepeatingFailure(t *testing.T) {
	var buf bytes.Buffer
	now := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	f := &failures{
		logger: slog.New(slog.NewTextHandler(&buf, nil)),
		every:  10 * time.Second,
		now:    func() time.Time { return now },
	}
	boom := errors.New("database down")
	for range 200 { // 10s of 50ms ticks, then one more
		f.report(t.Context(), "resolve rounds", boom)
		now = now.Add(50 * time.Millisecond)
	}
	f.report(t.Context(), "resolve rounds", boom)
	f.report(t.Context(), "resolve rounds", nil)
	f.report(t.Context(), "resolve rounds", nil)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("logged %d lines, want 3:\n%s", len(lines), buf.String())
	}
	if !strings.Contains(lines[0], "level=ERROR") || !strings.Contains(lines[0], "suppressed=0") {
		t.Errorf("first line = %q, want the first failure at once", lines[0])
	}
	if !strings.Contains(lines[1], "level=ERROR") || !strings.Contains(lines[1], "suppressed=199") {
		t.Errorf("second line = %q, want the failure again after 10s, 199 held back", lines[1])
	}
	if !strings.Contains(lines[2], "resolve rounds recovered") {
		t.Errorf("third line = %q, want the recovery", lines[2])
	}
}
