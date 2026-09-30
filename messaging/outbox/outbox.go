package outbox

import (
	"time"

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
func (o *Outbox) Sink() event.Sink[*sqlate.Tx] { return sink{o.eng} }

// Relay returns a relay over the outbox in db that waits poll between
// passes once a pass finds no row, and ends on a failure. poll and any
// [Timeout] must be positive, and any [Drain] not negative.
func (o *Outbox) Relay(db sqlate.Beginner, poll time.Duration, opts ...RelayOption) *Relay {
	r := &Relay{eng: o.eng, db: db, poll: poll, timeout: defaultTimeout}
	for _, opt := range opts {
		opt(r)
	}
	if r.poll <= 0 || r.timeout <= 0 || r.drain < 0 {
		panic("outbox: relay poll and timeout must be positive, and drain not negative")
	}
	return r
}
