package event_test

import (
	"context"
	"errors"
	"math"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/JaimeStill/spike-messaging/core/event"
)

// tx stands in for a transaction; the recorder only passes it through.
type tx struct{ name string }

// sink records what the recorder writes, and on which transaction.
type sink struct {
	tx     *tx
	events []event.Event
	err    error
}

func (s *sink) Write(_ context.Context, t *tx, es ...event.Event) error {
	if s.err != nil {
		return s.err
	}
	s.tx = t
	s.events = append(s.events, es...)
	return nil
}

type started struct {
	Round int `json:"round"`
}

var (
	kindStarted  = event.Define[started]("lab.exercise.started")
	kindStopped  = event.Define[struct{}]("lab.exercise.stopped")
	kindBadValue = event.Define[float64]("lab.exercise.bad")
)

func fixed(s *sink) *event.Recorder[*tx] {
	at := time.Date(2026, 9, 28, 12, 0, 0, 0, time.FixedZone("x", 3600))
	n := 0
	return event.NewRecorder[*tx](s, "/exercise",
		event.Clock(func() time.Time { return at }),
		event.IDs(func() string { n++; return strings.Repeat("a", n) }))
}

func TestEmitStampsAndWritesInRaiseOrder(t *testing.T) {
	s := &sink{}
	on := &tx{name: "t1"}
	run := fixed(s).Emit(t.Context(), func(got *tx, q *event.Queue) (string, error) {
		if got != on {
			t.Errorf("body ran on %v, want %v", got, on)
		}
		kindStarted.Raise(q, "ex-1", started{Round: 0})
		kindStopped.Raise(q, "ex-1", struct{}{})
		return "done", nil
	})
	out, err := run(on)
	if err != nil || out != "done" {
		t.Fatalf("run = %q, %v", out, err)
	}
	if s.tx != on {
		t.Errorf("wrote on %v, want %v", s.tx, on)
	}
	if len(s.events) != 2 {
		t.Fatalf("wrote %d events, want 2", len(s.events))
	}
	first, second := s.events[0], s.events[1]
	if first.Type != "lab.exercise.started" || second.Type != "lab.exercise.stopped" {
		t.Errorf("types = %q, %q; want raise order", first.Type, second.Type)
	}
	if first.ID != "a" || second.ID != "aa" {
		t.Errorf("ids = %q, %q", first.ID, second.ID)
	}
	want := time.Date(2026, 9, 28, 11, 0, 0, 0, time.UTC)
	if first.Source != "/exercise" || !first.Time.Equal(want) || first.Time.Location() != time.UTC {
		t.Errorf("stamp = %q %v", first.Source, first.Time)
	}
	if first.Subject != "ex-1" || first.DataContentType != "application/json" || string(first.Data) != `{"round":0}` {
		t.Errorf("event = %+v", first)
	}
}

func TestEmitWritesNothingWhenTheBodyFails(t *testing.T) {
	s := &sink{}
	boom := errors.New("refused")
	_, err := fixed(s).Emit(t.Context(), func(_ *tx, q *event.Queue) (int, error) {
		kindStarted.Raise(q, "ex-1", started{})
		return 0, boom
	})(&tx{})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the body's", err)
	}
	if len(s.events) != 0 {
		t.Errorf("wrote %d events after a failed body", len(s.events))
	}
}

func TestEmitWritesNothingWhenNothingIsRaised(t *testing.T) {
	s := &sink{err: errors.New("must not be called")}
	if _, err := fixed(s).Emit(t.Context(), func(*tx, *event.Queue) (int, error) { return 1, nil })(&tx{}); err != nil {
		t.Fatalf("err = %v", err)
	}
}

func TestEmitFailsTheCommandWhenTheSinkFails(t *testing.T) {
	boom := errors.New("insert failed")
	out, err := fixed(&sink{err: boom}).Emit(t.Context(), func(_ *tx, q *event.Queue) (string, error) {
		kindStarted.Raise(q, "ex-1", started{})
		return "done", nil
	})(&tx{})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the sink's", err)
	}
	if out != "" {
		t.Errorf("out = %q, want the zero result on failure", out)
	}
}

func TestEmitFailsTheCommandWhenARaiseFails(t *testing.T) {
	s := &sink{}
	_, err := fixed(s).Emit(t.Context(), func(_ *tx, q *event.Queue) (int, error) {
		kindStarted.Raise(q, "ex-1", started{})
		kindBadValue.Raise(q, "ex-1", math.NaN())
		if q.Err() == nil {
			t.Error("the queue holds no error after an unencodable raise")
		}
		if n := len(q.Events()); n != 1 {
			t.Errorf("queued %d events, want the one that encoded", n)
		}
		return 0, nil
	})(&tx{})
	if err == nil || !strings.Contains(err.Error(), "lab.exercise.bad") {
		t.Fatalf("err = %v, want the raise's", err)
	}
	if len(s.events) != 0 {
		t.Errorf("wrote %d events after a failed raise", len(s.events))
	}
}

func TestEmitMintsVersion7IDs(t *testing.T) {
	s := &sink{}
	_, err := event.NewRecorder[*tx](s, "/exercise").Emit(t.Context(), func(_ *tx, q *event.Queue) (int, error) {
		kindStarted.Raise(q, "", started{})
		kindStarted.Raise(q, "", started{})
		return 0, nil
	})(&tx{})
	if err != nil {
		t.Fatal(err)
	}
	v7 := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	for _, e := range s.events {
		if !v7.MatchString(e.ID) {
			t.Errorf("id %q is not a version 7 UUID", e.ID)
		}
		if e.Subject != "" {
			t.Errorf("subject = %q, want absent", e.Subject)
		}
	}
	if s.events[0].ID == s.events[1].ID {
		t.Error("two events share an id")
	}
}

func TestDefineRejectsABadType(t *testing.T) {
	for _, typ := range []string{"", "lab..x", "lab.*", "lab.>", "lab x"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Define(%q) did not panic", typ)
				}
			}()
			event.Define[started](typ)
		}()
	}
	if got := kindStarted.Type(); got != "lab.exercise.started" {
		t.Errorf("Type() = %q", got)
	}
}

func TestNewRecorderRejectsAWiringDefect(t *testing.T) {
	for name, build := range map[string]func(){
		"nil sink":     func() { event.NewRecorder[*tx](nil, "/exercise") },
		"empty source": func() { event.NewRecorder[*tx](&sink{}, "") },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s did not panic", name)
				}
			}()
			build()
		}()
	}
}
