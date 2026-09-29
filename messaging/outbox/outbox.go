package outbox

import (
	"github.com/standards-lab/sqlate"

	"github.com/JaimeStill/spike-messaging/core/event"
)

// Outbox runs the outbox on one engine's statements: the sink a recorder
// writes events through, and the relay that publishes them.
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

// Sink returns the [event.Sink] a composition root builds a service's
// [event.Recorder] over: it writes the events a command raises into the
// command's own transaction.
func (o *Outbox) Sink() event.Sink[*sqlate.Tx] { return sink{o} }

// Relay returns a relay over the outbox in db. Poll and Timeout must be
// positive.
func (o *Outbox) Relay(db sqlate.Beginner, opts ...RelayOption) *Relay {
	return newRelay(o, db, opts...)
}
