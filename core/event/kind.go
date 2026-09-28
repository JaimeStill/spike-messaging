package event

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// EventKind is one event a domain raises: its type, and T, the event entity
// its data carries. A domain declares its kinds with [Define], so the type
// and the entity cannot drift apart at a call site.
type EventKind[T any] struct {
	typ string
}

// Define declares the event kind of type typ, whose data is a T encoded as
// JSON. A type that breaks [CheckType] is a declaration defect and panics,
// at the package's initialization when the kind is a package variable.
func Define[T any](typ string) EventKind[T] {
	if err := CheckType(typ); err != nil {
		panic(fmt.Sprintf("event: define: %v", err))
	}
	return EventKind[T]{typ: typ}
}

// Type returns the kind's event type.
func (k EventKind[T]) Type() string { return k.typ }

// Raise adds an event of this kind about subject, carrying data, to q. The
// event has no id, source, or time until the recorder stamps it. An empty
// subject is an absent attribute. A failure to encode data is held on q, and
// fails the command when the recorder emits.
func (k EventKind[T]) Raise(q *Queue, subject string, data T) {
	b, err := json.Marshal(data)
	if err != nil {
		q.fail(fmt.Errorf("%s: encode: %w", k.typ, err))
		return
	}
	q.events = append(q.events, Event{
		Type:            k.typ,
		Subject:         subject,
		DataContentType: "application/json",
		Data:            b,
	})
}

// CheckType reports whether t is an event type a broker can route: one or
// more tokens separated by '.', none of them empty, with no whitespace, '*',
// or '>'. The rule is a subject's, so a provider can route on the type
// itself, and it admits the reverse-DNS types CloudEvents recommends, such as
// "lab.grant.approved".
func CheckType(t string) error {
	if t == "" {
		return errors.New("type: an empty type matches nothing")
	}
	for tok := range strings.SplitSeq(t, ".") {
		if tok == "" {
			return fmt.Errorf("type %q: an empty token", t)
		}
		if strings.ContainsFunc(tok, func(r rune) bool { return r == '*' || r == '>' || unicode.IsSpace(r) || unicode.IsControl(r) }) {
			return fmt.Errorf("type %q must not contain whitespace, '*', or '>'", t)
		}
	}
	return nil
}
