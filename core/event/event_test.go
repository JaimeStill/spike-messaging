package event_test

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/JaimeStill/spike-messaging/core/event"
)

func sample() event.Event {
	return event.Event{
		ID:              "7f3c",
		Source:          "/grants",
		Type:            "lab.grant.approved",
		Subject:         "grant/42",
		DataContentType: "application/json",
		Time:            time.Date(2026, 9, 23, 13, 0, 0, 123456789, time.UTC),
		Data:            []byte(`{"id":42}`),
		Extensions:      map[string]string{"traceparent": "00-abc-def-01"},
	}
}

func TestRoundTrip(t *testing.T) {
	cases := map[string]event.Event{
		"full":    sample(),
		"minimal": {ID: "1", Source: "/s", Type: "t"},
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			h, body, err := event.Encode(want)
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}
			got, err := event.Decode(h, body)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("round trip:\n got %+v\nwant %+v", got, want)
			}
		})
	}
}

func TestEncodeHeaders(t *testing.T) {
	h, _, err := event.Encode(sample())
	if err != nil {
		t.Fatal(err)
	}
	want := event.Header{
		"ce-specversion": {"1.0"},
		"ce-id":          {"7f3c"},
		"ce-source":      {"/grants"},
		"ce-type":        {"lab.grant.approved"},
		"ce-subject":     {"grant/42"},
		"ce-time":        {"2026-09-23T13:00:00.123456789Z"},
		"content-type":   {"application/json"},
		"ce-traceparent": {"00-abc-def-01"},
	}
	if !reflect.DeepEqual(h, want) {
		t.Errorf("headers:\n got %v\nwant %v", h, want)
	}
}

func TestDecodeCanonicalHTTPHeader(t *testing.T) {
	want := sample()
	h, body, err := event.Encode(want)
	if err != nil {
		t.Fatal(err)
	}
	hh := http.Header{}
	for k, vs := range h {
		hh.Set(k, vs[0])
	}
	got, err := event.Decode(event.Header(hh), body)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

func TestDecodeRejects(t *testing.T) {
	valid := event.Header{
		"ce-specversion": {"1.0"}, "ce-id": {"1"}, "ce-source": {"/s"}, "ce-type": {"t"},
	}
	with := func(k, v string) event.Header {
		h := event.Header{}
		for key, vs := range valid {
			h[key] = vs
		}
		if v == "" {
			delete(h, k)
		} else {
			h[k] = []string{v}
		}
		return h
	}
	cases := map[string]struct {
		h    event.Header
		want string
	}{
		"no specversion":    {with("ce-specversion", ""), "specversion"},
		"wrong specversion": {with("ce-specversion", "0.3"), "specversion"},
		"no id":             {with("ce-id", ""), "id is required"},
		"bad time":          {with("ce-time", "yesterday"), "time"},
		"bad extension":     {with("ce-Trace_Parent", "x"), "lowercase alphanumeric"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := event.Decode(c.h, nil)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %v, want it to mention %q", err, c.want)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	cases := map[string]struct {
		e    event.Event
		want []string
	}{
		"empty": {event.Event{}, []string{"id is required", "source is required", "type is required"}},
		"long extension": {
			event.Event{ID: "1", Source: "/s", Type: "t", Extensions: map[string]string{"abcdefghijklmnopqrstu": "x"}},
			[]string{"1 to 20 characters"},
		},
		"uppercase extension": {
			event.Event{ID: "1", Source: "/s", Type: "t", Extensions: map[string]string{"traceParent": "x"}},
			[]string{"lowercase alphanumeric"},
		},
		"reserved extension": {
			event.Event{ID: "1", Source: "/s", Type: "t", Extensions: map[string]string{"dataschema": "x"}},
			[]string{"context attribute"},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			err := c.e.Validate()
			if err == nil {
				t.Fatal("Validate: nil error")
			}
			for _, w := range c.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("err = %v, want it to mention %q", err, w)
				}
			}
			if _, _, encErr := event.Encode(c.e); encErr == nil {
				t.Error("Encode accepted an invalid event")
			}
		})
	}
	if err := sample().Validate(); err != nil {
		t.Errorf("sample: %v", err)
	}
}

func TestPermanent(t *testing.T) {
	if event.Permanent(nil) != nil {
		t.Error("Permanent(nil) is not nil")
	}
	if event.IsPermanent(fs.ErrNotExist) {
		t.Error("an unmarked error is permanent")
	}
	err := fmt.Errorf("handle: %w", event.Permanent(fs.ErrNotExist))
	if !event.IsPermanent(err) {
		t.Error("the mark did not survive wrapping")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Error("errors.Is does not reach the cause")
	}
	var pe *fs.PathError
	cause := event.Permanent(&fs.PathError{Op: "open", Path: "x", Err: fs.ErrNotExist})
	if !errors.As(cause, &pe) {
		t.Error("errors.As does not reach the cause")
	}
}
