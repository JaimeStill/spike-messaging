// Package memory is the in-memory messaging provider: the conformance double
// for the standard tier and a test double for a service.
//
// A [Broker] keeps one append-only log of encoded events, as a JetStream
// stream would, and a durable consumer for each subscription Name that
// starts at the log's beginning. Publish encodes an event to binary content
// mode and each delivery decodes it, so the codec a real binding uses is
// exercised on every message. Nothing persists beyond the process.
package memory

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/JaimeStill/spike-messaging/core/event"
	"github.com/JaimeStill/spike-messaging/core/reactor"
	"github.com/JaimeStill/spike-messaging/messaging"
)

// DefaultAckWait is the AckWait of a subscription that sets none.
const DefaultAckWait = 30 * time.Second

// Broker is an in-memory [messaging.Broker]. Its zero value is not usable;
// call [New].
type Broker struct {
	mu        sync.Mutex
	log       []message
	consumers map[string]*consumer
}

type message struct {
	typ    string
	header event.Header
	body   []byte
}

// New returns an empty broker.
func New() *Broker {
	return &Broker{consumers: map[string]*consumer{}}
}

var _ messaging.Broker = (*Broker)(nil)

// Publish appends e to the log and wakes every consumer.
func (b *Broker) Publish(_ context.Context, e event.Event) error {
	h, body, err := event.Encode(e)
	if err != nil {
		return fmt.Errorf("memory: publish: %w", err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.log = append(b.log, message{typ: e.Type, header: h, body: body})
	for _, c := range b.consumers {
		c.wakeAll()
	}
	return nil
}

// Subscribe returns a source on the durable consumer sub names, creating it
// on first use. Sources under one Name share its position and split its
// work. A later subscription under an existing Name must match the first,
// as binding to a JetStream durable must.
func (b *Broker) Subscribe(sub messaging.Subscription) (reactor.Source[event.Event], error) {
	if err := sub.Validate(); err != nil {
		return nil, err
	}
	if sub.AckWait == 0 {
		sub.AckWait = DefaultAckWait
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	c, ok := b.consumers[sub.Name]
	if !ok {
		c = &consumer{
			sub:     sub,
			pending: map[int]delivery{},
			counts:  map[int]int{},
			wake:    make(chan struct{}),
		}
		b.consumers[sub.Name] = c
	} else if !reflect.DeepEqual(c.sub, sub) {
		return nil, fmt.Errorf("memory: subscription %q exists with a different configuration", sub.Name)
	}
	return &source{b: b, c: c}, nil
}

// consumer is one durable consumer. The broker's mutex guards it.
type consumer struct {
	sub     messaging.Subscription
	cursor  int              // the next sequence never delivered
	pending map[int]delivery // sequence to its delivery in flight
	retries []retry          // redeliveries waiting to come due
	counts  map[int]int      // sequence to deliveries made, while unsettled
	tokens  uint64
	wake    chan struct{} // closed when there may be new work
}

type delivery struct {
	token    uint64
	deadline time.Time
}

type retry struct {
	seq int
	due time.Time
}

func (c *consumer) wakeAll() {
	close(c.wake)
	c.wake = make(chan struct{})
}

// expire moves each delivery past its AckWait to the retries, due now, so
// any late outcome is ignored.
func (c *consumer) expire(now time.Time) {
	for seq, d := range c.pending {
		if !now.Before(d.deadline) {
			delete(c.pending, seq)
			c.redeliver(seq, now)
		}
	}
}

// redeliver schedules seq for another delivery at due, or terminates it
// once MaxDeliver deliveries have been made.
func (c *consumer) redeliver(seq int, due time.Time) {
	if c.sub.MaxDeliver > 0 && c.counts[seq] >= c.sub.MaxDeliver {
		delete(c.counts, seq)
		return
	}
	c.retries = append(c.retries, retry{seq: seq, due: due})
	c.wakeAll()
}

// take claims the next event for delivery: a due retry first, then the next
// matching event past the cursor. With nothing to take, it returns the
// channel that signals new work and how long until a retry comes due or a
// delivery expires, or 0 when nothing is scheduled.
func (b *Broker) take(c *consumer, now time.Time) (seq int, d delivery, ok bool, wake <-chan struct{}, wait time.Duration) {
	c.expire(now)
	pick := -1
	for i, r := range c.retries {
		if !now.Before(r.due) && (pick < 0 || r.due.Before(c.retries[pick].due)) {
			pick = i
		}
	}
	switch {
	case pick >= 0:
		seq = c.retries[pick].seq
		c.retries = append(c.retries[:pick], c.retries[pick+1:]...)
	default:
		for c.cursor < len(b.log) && !c.sub.Matches(b.log[c.cursor].typ) {
			c.cursor++ // acknowledged on sight
		}
		if c.cursor == len(b.log) {
			return 0, delivery{}, false, c.wake, c.nextDue(now)
		}
		seq = c.cursor
		c.cursor++
	}
	c.counts[seq]++
	c.tokens++
	d = delivery{token: c.tokens, deadline: now.Add(c.sub.AckWait)}
	c.pending[seq] = d
	return seq, d, true, nil, 0
}

func (c *consumer) nextDue(now time.Time) time.Duration {
	var next time.Time
	for _, r := range c.retries {
		if next.IsZero() || r.due.Before(next) {
			next = r.due
		}
	}
	for _, d := range c.pending {
		if next.IsZero() || d.deadline.Before(next) {
			next = d.deadline
		}
	}
	if next.IsZero() {
		return 0
	}
	return max(next.Sub(now), time.Millisecond)
}

// settle applies a handler's outcome to the delivery it came from. An
// outcome for a delivery that expired is ignored: the event has been, or
// will be, redelivered.
func (b *Broker) settle(c *consumer, seq int, d delivery, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	c.expire(now)
	if cur, ok := c.pending[seq]; !ok || cur.token != d.token {
		return
	}
	delete(c.pending, seq)
	if err == nil || event.IsPermanent(err) {
		delete(c.counts, seq)
		return
	}
	c.redeliver(seq, now.Add(c.sub.RetryDelay))
}

// source is one member of a consumer.
type source struct {
	b     *Broker
	c     *consumer
	ready atomic.Bool
}

// Receive delivers the consumer's events to fn one at a time until ctx
// ends. Each delivery's context carries its AckWait deadline. A handler
// error never ends Receive; the delivery's outcome follows
// [messaging.Broker]'s rule.
func (s *source) Receive(ctx context.Context, fn reactor.Func[event.Event]) error {
	s.ready.Store(true)
	defer s.ready.Store(false)
	for {
		if ctx.Err() != nil {
			return nil
		}
		s.b.mu.Lock()
		seq, d, ok, wake, wait := s.b.take(s.c, time.Now())
		var msg message
		if ok {
			msg = s.b.log[seq]
		}
		s.b.mu.Unlock()

		if !ok {
			var due <-chan time.Time
			var t *time.Timer
			if wait > 0 {
				t = time.NewTimer(wait)
				due = t.C
			}
			select {
			case <-ctx.Done():
			case <-wake:
			case <-due:
			}
			if t != nil {
				t.Stop()
			}
			continue
		}

		e, err := event.Decode(msg.header, msg.body)
		if err != nil {
			// A message that cannot decode can never be handled.
			s.b.settle(s.c, seq, d, event.Permanent(err))
			continue
		}
		hctx, cancel := context.WithDeadline(ctx, d.deadline)
		err = fn(hctx, e)
		cancel()
		s.b.settle(s.c, seq, d, err)
	}
}

// Ready reports whether the source is receiving.
func (s *source) Ready() bool { return s.ready.Load() }
