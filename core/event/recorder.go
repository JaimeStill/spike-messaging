package event

import (
	"context"
	"fmt"
	"time"
	"uuid"
)

// Sink writes stamped events into a transaction of type Tx, the one the
// command that raised them runs in. An outbox is the sink a composition
// root builds a recorder over; it writes each event as a row the relay
// publishes after the transaction commits.
type Sink[Tx any] interface {
	Write(ctx context.Context, tx Tx, es ...Event) error
}

// Recorder emits the events a command raises into the command's
// transaction, stamped with the id, source, and time the domain never sets.
// The composition root builds one per service and injects it into each
// domain.
type Recorder[Tx any] struct {
	sink   Sink[Tx]
	source string
	now    func() time.Time
	id     func() string
}

// Option configures a [Recorder].
type Option func(*options)

type options struct {
	now func() time.Time
	id  func() string
}

// Clock sets the time a recorder stamps; the default is time.Now.
func Clock(now func() time.Time) Option { return func(o *options) { o.now = now } }

// IDs sets how a recorder mints an event id; the default is a version 7
// UUID, which orders by time.
func IDs(id func() string) Option { return func(o *options) { o.id = id } }

// NewRecorder builds the recorder that writes through sink and stamps every
// event with source, the service's own URI-reference. Source is one value for
// the whole service, never a replica's, because a consumer deduplicates on
// the source and id together. A nil sink or an empty source is a wiring
// defect and panics.
func NewRecorder[Tx any](sink Sink[Tx], source string, opts ...Option) *Recorder[Tx] {
	if sink == nil {
		panic("event: recorder: nil sink")
	}
	if source == "" {
		panic("event: recorder: empty source")
	}
	o := options{now: time.Now, id: func() string { return uuid.NewV7().String() }}
	for _, opt := range opts {
		opt(&o)
	}
	return &Recorder[Tx]{sink: sink, source: source, now: o.now, id: o.id}
}

// Emit wraps fn, a command's body, into the function a transaction runs, such
// as sqlate's Transact. The body runs on the transaction with a fresh
// [Queue]. When it returns an error, Emit returns it and writes nothing.
// When it succeeds, Emit stamps each queued event and writes them, in the
// order raised, through the sink into the same transaction, before the
// caller commits. A failed raise or write fails the command, so the
// transaction rolls back with its state and its events together. ctx is the
// context of the write.
func (r *Recorder[Tx]) Emit[R any](ctx context.Context, fn func(Tx, *Queue) (R, error)) func(Tx) (R, error) {
	return func(tx Tx) (R, error) {
		var zero R
		q := &Queue{}
		out, err := fn(tx, q)
		if err != nil {
			return zero, err
		}
		if err := q.Err(); err != nil {
			return zero, fmt.Errorf("event: raise: %w", err)
		}
		if len(q.events) == 0 {
			return out, nil
		}
		now := r.now().UTC()
		es := make([]Event, len(q.events))
		for i, e := range q.events {
			e.ID, e.Source, e.Time = r.id(), r.source, now
			if err := e.Validate(); err != nil {
				return zero, err
			}
			es[i] = e
		}
		if err := r.sink.Write(ctx, tx, es...); err != nil {
			return zero, fmt.Errorf("event: emit: %w", err)
		}
		return out, nil
	}
}
