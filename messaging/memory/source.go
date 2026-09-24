package memory

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/JaimeStill/spike-messaging/core/event"
	"github.com/JaimeStill/spike-messaging/core/reactor"
)

// source is one member of a consumer: the loop a reactor runs.
type source struct {
	b     *Broker
	c     *consumer
	ready atomic.Bool
}

// Receive delivers the consumer's events to fn one at a time until ctx
// ends. Each delivery's context carries its AckWait deadline. A handler
// error never ends Receive; the delivery's outcome follows
// [messaging.Broker]'s rule. A member settles the event it is handling
// before it next checks ctx, so a drained handler's outcome holds.
func (s *source) Receive(ctx context.Context, fn reactor.Func[event.Event]) error {
	s.ready.Store(true)
	defer s.ready.Store(false)
	for ctx.Err() == nil {
		s.b.mu.Lock()
		seq, d, ok, wake, wait := s.c.take(s.b.log, time.Now())
		var msg message
		if ok {
			msg = s.b.log[seq]
		}
		s.b.mu.Unlock()

		if !ok {
			s.await(ctx, wake, wait)
			continue
		}

		e, err := event.Decode(msg.header, msg.body)
		if err != nil {
			// A message that cannot decode can never be handled.
			s.settle(seq, d, event.Permanent(err))
			continue
		}
		hctx, cancel := context.WithDeadline(ctx, d.deadline)
		err = fn(hctx, e)
		cancel()
		s.settle(seq, d, err)
	}
	return nil
}

// await blocks until ctx ends, the consumer may have new work, or wait
// passes; a wait of 0 has no timer.
func (s *source) await(ctx context.Context, wake <-chan struct{}, wait time.Duration) {
	var due <-chan time.Time
	if wait > 0 {
		t := time.NewTimer(wait)
		defer t.Stop()
		due = t.C
	}
	select {
	case <-ctx.Done():
	case <-wake:
	case <-due:
	}
}

func (s *source) settle(seq int, d delivery, err error) {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	s.c.settle(seq, d, err, time.Now())
}

// Ready reports whether the source is receiving.
func (s *source) Ready() bool { return s.ready.Load() }
