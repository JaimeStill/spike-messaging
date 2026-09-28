package event

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"time"
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
	o := options{now: time.Now, id: func() string { return uuidV7(time.Now()) }}
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
			return out, err
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

// uuidV7 returns a version 7 UUID for t: 48 bits of Unix milliseconds, then
// random bits, so ids sort by the time they were minted.
func uuidV7(t time.Time) string {
	var b [16]byte
	var ms [8]byte
	binary.BigEndian.PutUint64(ms[:], uint64(t.UnixMilli()))
	copy(b[:6], ms[2:])
	_, _ = rand.Read(b[6:])
	b[6] = b[6]&0x0f | 0x70
	b[8] = b[8]&0x3f | 0x80
	var s [36]byte
	hex.Encode(s[0:8], b[0:4])
	s[8] = '-'
	hex.Encode(s[9:13], b[4:6])
	s[13] = '-'
	hex.Encode(s[14:18], b[6:8])
	s[18] = '-'
	hex.Encode(s[19:23], b[8:10])
	s[23] = '-'
	hex.Encode(s[24:], b[10:])
	return string(s[:])
}
