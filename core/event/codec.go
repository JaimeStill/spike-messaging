package event

import (
	"fmt"
	"strings"
	"time"
)

// Header is a message's headers in binary content mode. Its shape matches
// nats.Header and http.Header. Values are written as given: a provider whose
// protocol binding requires encoding them, such as HTTP's percent-encoding,
// encodes them itself.
type Header map[string][]string

const (
	prefix      = "ce-"
	contentType = "content-type"
)

// Encode maps a valid e to binary content mode: the headers and the body.
func Encode(e Event) (Header, []byte, error) {
	if err := e.Validate(); err != nil {
		return nil, nil, err
	}
	h := Header{
		prefix + "specversion": {SpecVersion},
		prefix + "id":          {e.ID},
		prefix + "source":      {e.Source},
		prefix + "type":        {e.Type},
	}
	if e.DataSchema != "" {
		h[prefix+"dataschema"] = []string{e.DataSchema}
	}
	if e.Subject != "" {
		h[prefix+"subject"] = []string{e.Subject}
	}
	if !e.Time.IsZero() {
		h[prefix+"time"] = []string{e.Time.UTC().Format(time.RFC3339Nano)}
	}
	if e.DataContentType != "" {
		h[contentType] = []string{e.DataContentType}
	}
	for name, v := range e.Extensions {
		h[prefix+name] = []string{v}
	}
	return h, e.Data, nil
}

// Decode reads an event from binary content mode. Header names match
// case-insensitively, so a canonicalized HTTP header decodes too, and a
// lowercase name wins over another spelling of it; any
// ce-prefixed header that is not a context attribute is an extension. The
// result must carry specversion 1.0 and pass [Event.Validate].
func Decode(h Header, body []byte) (Event, error) {
	var e Event
	var version string
	for key, vs := range h {
		if len(vs) == 0 {
			continue
		}
		lower, v := strings.ToLower(key), vs[0]
		if lower != key {
			if _, ok := h[lower]; ok {
				continue
			}
		}
		key = lower
		if key == contentType {
			e.DataContentType = v
			continue
		}
		name, ok := strings.CutPrefix(key, prefix)
		if !ok {
			continue
		}
		switch name {
		case "specversion":
			version = v
		case "id":
			e.ID = v
		case "source":
			e.Source = v
		case "type":
			e.Type = v
		case "dataschema":
			e.DataSchema = v
		case "subject":
			e.Subject = v
		case "time":
			t, err := time.Parse(time.RFC3339Nano, v)
			if err != nil {
				return Event{}, fmt.Errorf("event: time: %w", err)
			}
			e.Time = t
		default:
			if e.Extensions == nil {
				e.Extensions = map[string]string{}
			}
			e.Extensions[name] = v
		}
	}
	if version != SpecVersion {
		return Event{}, fmt.Errorf("event: specversion %q, want %q", version, SpecVersion)
	}
	if len(body) > 0 {
		e.Data = body
	}
	if err := e.Validate(); err != nil {
		return Event{}, err
	}
	return e, nil
}
