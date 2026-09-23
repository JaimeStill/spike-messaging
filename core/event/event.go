package event

import (
	"errors"
	"fmt"
	"time"
)

// SpecVersion is the CloudEvents specification version this package
// implements.
const SpecVersion = "1.0"

// Event is one CloudEvents 1.0 event. ID, Source, and Type are required; the
// specversion is always [SpecVersion] and is not a field. A zero Time and an
// empty Subject, DataContentType, or DataSchema are absent attributes. Extensions holds
// the extension attributes, such as traceparent, keyed by name.
type Event struct {
	ID              string
	Source          string
	Type            string
	Subject         string
	DataContentType string
	DataSchema      string
	Time            time.Time
	Data            []byte
	Extensions      map[string]string
}

// reserved names the context attributes an extension may not shadow.
var reserved = map[string]bool{
	"id": true, "source": true, "specversion": true, "type": true,
	"datacontenttype": true, "dataschema": true, "subject": true,
	"time": true, "data": true,
}

// Validate reports every way e breaks the specification: a missing required
// attribute, or an extension name that is empty, is not lowercase
// alphanumeric, or shadows a context attribute. The specification's
// 20-character limit on names is a recommendation, so it is not enforced.
func (e Event) Validate() error {
	var errs []error
	if e.ID == "" {
		errs = append(errs, errors.New("id is required"))
	}
	if e.Source == "" {
		errs = append(errs, errors.New("source is required"))
	}
	if e.Type == "" {
		errs = append(errs, errors.New("type is required"))
	}
	for name := range e.Extensions {
		if err := validName(name); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("event: invalid: %w", errors.Join(errs...))
	}
	return nil
}

func validName(name string) error {
	if name == "" {
		return errors.New("extension: name must not be empty")
	}
	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return fmt.Errorf("extension %q: name must be lowercase alphanumeric", name)
		}
	}
	if reserved[name] {
		return fmt.Errorf("extension %q: name is a context attribute", name)
	}
	return nil
}
