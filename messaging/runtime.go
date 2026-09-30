package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/standards-lab/sqlate"

	"github.com/JaimeStill/spike-messaging/core/event"
	"github.com/JaimeStill/spike-messaging/core/reactor"
	"github.com/JaimeStill/spike-messaging/messaging/inbox"
	"github.com/JaimeStill/spike-messaging/messaging/outbox"
)

// Runtime is one service's messaging, built from its [Config], the broker
// its provider supplies, and its database engine's outbox and inbox
// statements. The broker carries events to and from the service. The outbox
// holds the events a command emits until the relay publishes them, and the
// inbox records the events a consumer has handled. A domain takes Recorder,
// its one value for emitting events, and records each event in its
// command's transaction.
//
// A Runtime registers nothing: the composition root registers the broker,
// the engine's statement verification, and each reactor the runtime
// builds, each at the stage the root chooses.
type Runtime struct {
	Recorder *event.Recorder[*sqlate.Tx]

	broker   Broker
	outbox   *outbox.Outbox
	inbox    *inbox.Inbox
	cfg      Config
	shutdown time.Duration
	logger   *slog.Logger
}

// New builds a service's messaging from cfg, over broker and the engine's
// statements, logging to logger. shutdown is the process's drain timeout,
// which every reactor the runtime builds drains within. New checks cfg,
// which is finalized, and shutdown, which must be positive. It does no I/O.
func New(cfg Config, broker Broker, outboxEngine outbox.Engine, inboxEngine inbox.Engine, shutdown time.Duration, logger *slog.Logger) (*Runtime, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("messaging: config: %w", err)
	}
	if shutdown <= 0 {
		return nil, fmt.Errorf("messaging: shutdown must be positive, got %v", shutdown)
	}
	ob, err := outbox.New(outboxEngine)
	if err != nil {
		return nil, fmt.Errorf("messaging: %w", err)
	}
	in, err := inbox.New(inboxEngine)
	if err != nil {
		return nil, fmt.Errorf("messaging: %w", err)
	}
	return &Runtime{
		Recorder: event.NewRecorder(ob.Sink(), cfg.Source),
		broker:   broker,
		outbox:   ob,
		inbox:    in,
		cfg:      cfg,
		shutdown: shutdown,
		logger:   logger,
	}, nil
}

// Relay returns the reactor that publishes the outbox's committed events on
// db to the broker, with the grace [reactor.GraceWithin] gives the drain
// timeout. The relay makes a last pass, bounded at a quarter of the drain
// timeout and so below its grace, that publishes what the producers
// committed while they drained. The root therefore registers the relay
// below every stage that commits events. The relay logs each event it
// publishes at info with its type, id, and subject, so the service's log
// shows the traffic it sends.
func (r *Runtime) Relay(db sqlate.Beginner) *reactor.Reactor[event.Event] {
	src := r.outbox.Relay(db, r.cfg.RelayPoll.Duration(), outbox.Drain(r.shutdown/4))
	publish := func(ctx context.Context, e event.Event) error {
		if err := r.broker.Publish(ctx, e); err != nil {
			return err
		}
		r.logger.InfoContext(ctx, "event published", "type", e.Type, "event", e.ID, "subject", e.Subject)
		return nil
	}
	return reactor.New(src, publish, reactor.GraceWithin(r.shutdown))
}

// Claim records, in a command's transaction, that the consumer is handling
// the event it was bound to, and reports whether this is the first time. A
// command runs it first and, on false, commits nothing more. It is an
// alias, so a domain's own claim type of the same shape accepts it.
type Claim = func(ctx context.Context, tx *sqlate.Tx) (first bool, err error)

// Consume returns the reactor that consumes sub's events, with the grace
// [reactor.GraceWithin] gives the drain timeout. The reactor decodes each
// event's data into T, the consumer's own type for the payload, and hands it
// to fn with a [Claim] over the inbox under sub.Name, bound to the event, so
// fn's domain never sees the event and a redelivery changes nothing. Data
// that does not decode is refused with [event.Permanent], because no
// redelivery can fix it.
//
// The reactor logs each delivery with the consumer, the event's type, id,
// and subject, and its outcome, so the service's log shows the traffic it
// receives. The outcome is "event consumed" at info when handled; repeat,
// when the claim found the event handled already; or retried, with the
// error, when the broker will redeliver it. A permanent refusal, from the
// decode or from fn, is "event refused" at warn with the error, because the
// broker terminates the delivery and nothing else reports it.
func (r *Runtime) Consume[T any](sub Subscription, fn func(ctx context.Context, data T, claim Claim) error) (*reactor.Reactor[event.Event], error) {
	src, err := r.broker.Subscribe(sub)
	if err != nil {
		return nil, err
	}
	handle := func(ctx context.Context, e event.Event) error {
		var data T
		repeat := false
		err := json.Unmarshal(e.Data, &data)
		if err != nil {
			err = event.Permanent(fmt.Errorf("decode: %w", err))
		} else {
			claim := func(ctx context.Context, tx *sqlate.Tx) (bool, error) {
				first, err := r.inbox.Claim(ctx, tx, sub.Name, e)
				repeat = err == nil && !first
				return first, err
			}
			err = fn(ctx, data, claim)
		}
		attrs := []any{"consumer", sub.Name, "type", e.Type, "event", e.ID, "subject", e.Subject}
		switch {
		case event.IsPermanent(err):
			r.logger.WarnContext(ctx, "event refused", append(attrs, "outcome", "refused", "error", err)...)
		case err != nil:
			r.logger.InfoContext(ctx, "event consumed", append(attrs, "outcome", "retried", "error", err)...)
		case repeat:
			r.logger.InfoContext(ctx, "event consumed", append(attrs, "outcome", "repeat")...)
		default:
			r.logger.InfoContext(ctx, "event consumed", append(attrs, "outcome", "handled")...)
		}
		return err
	}
	return reactor.New(src, handle, reactor.GraceWithin(r.shutdown)), nil
}
