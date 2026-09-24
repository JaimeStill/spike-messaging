package outbox

import (
	"github.com/standards-lab/sqlate"

	"github.com/JaimeStill/spike-messaging/core/event"
)

// Outbox runs the outbox on one engine's statements: the emitter a domain
// writes events with, the relay that publishes them, and the inbox a
// handler claims events in.
type Outbox struct {
	eng Engine
}

// New returns an outbox over eng, once eng defines every statement with the
// parameters [Engine] names.
func New(eng Engine) (*Outbox, error) {
	if err := eng.validate(); err != nil {
		return nil, err
	}
	return &Outbox{eng: eng}, nil
}

// Emitter returns the [event.Emitter] a composition root injects into a
// domain.
func (o *Outbox) Emitter() event.Emitter { return emitter{o} }

// Relay returns a relay over the outbox in db. Poll and Timeout must be
// positive.
func (o *Outbox) Relay(db sqlate.Beginner, opts ...RelayOption) *Relay {
	return newRelay(o, db, opts...)
}
