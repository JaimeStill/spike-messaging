package nats

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	natsgo "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/JaimeStill/spike-messaging/core/event"
	"github.com/JaimeStill/spike-messaging/core/reactor"
	"github.com/JaimeStill/spike-messaging/messaging"
)

const (
	// pullWait bounds one pull, so a pull the server holds for a member that
	// has stopped expires soon.
	pullWait = time.Second
	// settleWait bounds the wait for the server to confirm an outcome.
	settleWait = 5 * time.Second
	// retryWait is how long a member waits after a pull fails before it
	// pulls again.
	retryWait = 100 * time.Millisecond
	// failuresBeforeCheck is how many pulls in a row may fail before the
	// member asks the server whether its consumer still exists.
	failuresBeforeCheck = 3
)

// source is one member of a durable consumer: the loop a reactor runs.
type source struct {
	b     *Broker
	sub   messaging.Subscription
	cfg   jetstream.ConsumerConfig
	ready atomic.Bool
}

// Receive binds the consumer, then delivers its messages to fn one at a
// time until ctx ends. It fails when the binding does, as when the Name's
// consumer has another configuration, when the connection closes, or when
// the consumer or its stream is deleted. Any other failed pull is retried,
// and the source reports not ready until a pull succeeds again. A handler
// error never ends Receive.
func (s *source) Receive(ctx context.Context, fn reactor.Func[event.Event]) error {
	c := s.b.conn.Load()
	if c == nil {
		return ErrNotStarted
	}
	cons, err := c.js.CreateConsumer(ctx, s.b.cfg.Stream, s.cfg)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("nats: bind consumer %s: %w", s.sub.Name, err)
	}
	s.ready.Store(true)
	defer s.ready.Store(false)
	failures := 0
	for ctx.Err() == nil {
		// The pull's context ends with ctx, and its deadline sets the pull's
		// expiry on the server.
		pctx, cancel := context.WithTimeout(ctx, pullWait)
		msg, err := cons.Next(jetstream.FetchContext(pctx))
		cancel()
		switch {
		case err == nil:
			failures = 0
			s.ready.Store(true)
			s.deliver(ctx, msg, fn)
		case ctx.Err() != nil, errors.Is(err, context.DeadlineExceeded), errors.Is(err, natsgo.ErrTimeout), errors.Is(err, jetstream.ErrNoMessages):
			// An empty pull is a healthy one.
			failures = 0
			s.ready.Store(true)
		case errors.Is(err, natsgo.ErrConnectionClosed), errors.Is(err, jetstream.ErrConsumerDeleted):
			return fmt.Errorf("nats: receive %s: %w", s.sub.Name, err)
		default:
			// Most failures pass, such as a reconnect or a leader election.
			// A deleted consumer or stream does not: its pulls fail with no
			// responders, so after a few failures the member asks the server.
			s.ready.Store(false)
			failures++
			if errors.Is(err, natsgo.ErrNoResponders) || failures >= failuresBeforeCheck {
				if gone := s.gone(ctx, cons); gone != nil {
					return fmt.Errorf("nats: receive %s: %w", s.sub.Name, gone)
				}
			}
			select {
			case <-ctx.Done():
			case <-time.After(retryWait):
			}
		}
	}
	return nil
}

// gone returns the error that says cons or its stream no longer exists, or
// nil when it does or the server cannot say.
func (s *source) gone(ctx context.Context, cons jetstream.Consumer) error {
	ictx, cancel := context.WithTimeout(ctx, pullWait)
	defer cancel()
	_, err := cons.Info(ictx)
	if errors.Is(err, jetstream.ErrConsumerNotFound) || errors.Is(err, jetstream.ErrStreamNotFound) {
		return err
	}
	return nil
}

// deliver handles one message and settles its outcome, unless the handler
// missed its deadline, in which case the outcome is dropped and the server
// redelivers the message.
func (s *source) deliver(ctx context.Context, msg jetstream.Msg, fn reactor.Func[event.Event]) {
	deadline := time.Now().Add(s.cfg.AckWait - AckMargin)
	e, err := event.Decode(event.Header(msg.Headers()), msg.Data())
	if err != nil {
		// A message that cannot decode can never be handled.
		_ = msg.Term()
		return
	}
	hctx, cancel := context.WithDeadline(ctx, deadline)
	err = fn(hctx, e)
	cancel()
	if !time.Now().Before(deadline) {
		return
	}
	switch {
	case err == nil:
		sctx, cancel := context.WithTimeout(context.Background(), settleWait)
		_ = msg.DoubleAck(sctx)
		cancel()
	case event.IsPermanent(err):
		_ = msg.Term()
	case s.sub.RetryDelay > 0:
		_ = msg.NakWithDelay(s.sub.RetryDelay)
	default:
		_ = msg.Nak()
	}
}

// Ready reports whether the source is receiving.
func (s *source) Ready() bool { return s.ready.Load() }
