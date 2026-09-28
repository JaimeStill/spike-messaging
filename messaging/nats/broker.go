package nats

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	natsgo "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/JaimeStill/spike-messaging/core/event"
	"github.com/JaimeStill/spike-messaging/core/reactor"
	"github.com/JaimeStill/spike-messaging/messaging"
)

const (
	// DefaultDuplicates is the stream's deduplication window when Config
	// sets none: JetStream's own default.
	DefaultDuplicates = 2 * time.Minute
	// DefaultAckWait is the AckWait of a subscription that sets none.
	DefaultAckWait = 30 * time.Second
	// AckMargin is how much longer the consumer waits for an outcome than
	// the handler's deadline, so an outcome settled in time reaches the
	// server before it redelivers, on a link whose round trip is well under
	// the margin. On a slower link a settled event can be delivered again,
	// never lost. The margin is part of the consumer's configuration, so a
	// release that changes it cannot bind the durables an earlier one made.
	AckMargin = 250 * time.Millisecond
)

// Config names the stream a broker publishes to and subscribes on.
type Config struct {
	// Stream is the stream's name: a token, as a subscription's Name is.
	Stream string
	// Prefix is the subject prefix, one or more tokens; the stream captures
	// Prefix.> and an event is published to Prefix.<type>.
	Prefix string
	// Duplicates is the stream's deduplication window; 0 is
	// DefaultDuplicates.
	Duplicates time.Duration
	// MaxAge bounds the stream's retention: an event older than it is
	// discarded, whether or not every consumer has received it. A consumer
	// that falls behind it recovers through its producer republishing its
	// current state. 0 keeps every event, and a positive MaxAge must be at
	// least the deduplication window, as JetStream requires.
	MaxAge time.Duration
}

// Validate reports every way cfg is unusable.
func (cfg Config) Validate() error {
	var errs []error
	if !messaging.IsToken(cfg.Stream) {
		errs = append(errs, fmt.Errorf("stream %q must be a token, as a subscription's name is", cfg.Stream))
	}
	if err := messaging.CheckType(cfg.Prefix); err != nil {
		errs = append(errs, fmt.Errorf("prefix: %w", err))
	}
	if cfg.Duplicates < 0 {
		errs = append(errs, errors.New("duplicates must not be negative"))
	}
	dupes := cfg.Duplicates
	if dupes == 0 {
		dupes = DefaultDuplicates
	}
	switch {
	case cfg.MaxAge < 0:
		errs = append(errs, errors.New("max age must not be negative"))
	case cfg.MaxAge > 0 && cfg.MaxAge < dupes:
		errs = append(errs, fmt.Errorf("max age %v must be at least the deduplication window, %v", cfg.MaxAge, dupes))
	}
	if len(errs) > 0 {
		return fmt.Errorf("nats: config: %w", errors.Join(errs...))
	}
	return nil
}

// Broker is a [messaging.Broker] on a JetStream stream.
type Broker struct {
	nc  *natsgo.Conn
	js  jetstream.JetStream
	cfg Config
}

var _ messaging.Broker = (*Broker)(nil)

// New provisions cfg's stream on nc, creating it or updating it to cfg, and
// returns a broker on it. The broker owns nc from here: its Shutdown drains
// it.
func New(ctx context.Context, nc *natsgo.Conn, cfg Config) (*Broker, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if cfg.Duplicates == 0 {
		cfg.Duplicates = DefaultDuplicates
	}
	js, err := jetstream.New(nc)
	if err != nil {
		return nil, fmt.Errorf("nats: %w", err)
	}
	_, err = js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:       cfg.Stream,
		Subjects:   []string{cfg.Prefix + ".>"},
		Storage:    jetstream.FileStorage,
		Retention:  jetstream.LimitsPolicy,
		Duplicates: cfg.Duplicates,
		MaxAge:     cfg.MaxAge,
	})
	if err != nil {
		return nil, fmt.Errorf("nats: provision stream %s: %w", cfg.Stream, err)
	}
	return &Broker{nc: nc, js: js, cfg: cfg}, nil
}

// Conn is the native handle, for a use beyond the standard tier, such as
// request and reply.
func (b *Broker) Conn() *natsgo.Conn { return b.nc }

// Ready reports whether the connection is up.
func (b *Broker) Ready() bool { return b.nc.IsConnected() }

// Shutdown drains the connection: its subscriptions stop, pending
// publishes and acknowledgements flush, and it closes. It returns once the
// connection is closed. When ctx ends first, it closes the connection
// without finishing the drain and returns ctx's error.
func (b *Broker) Shutdown(ctx context.Context) error {
	// A connection that is reconnecting cannot drain, and Drain closes it.
	if err := b.nc.Drain(); err != nil && !errors.Is(err, natsgo.ErrConnectionClosed) && !errors.Is(err, natsgo.ErrConnectionReconnecting) {
		b.nc.Close()
		return fmt.Errorf("nats: drain: %w", err)
	}
	t := time.NewTicker(10 * time.Millisecond)
	defer t.Stop()
	for !b.nc.IsClosed() {
		select {
		case <-ctx.Done():
			b.nc.Close()
			return fmt.Errorf("nats: drain: %w", ctx.Err())
		case <-t.C:
		}
	}
	return nil
}

// Publish publishes e to Prefix.<type> with its source and id as
// Nats-Msg-Id, and returns once the stream holds it. A repeat of a source and
// id within the stream's deduplication window is accepted and dropped.
func (b *Broker) Publish(ctx context.Context, e event.Event) error {
	h, body, err := event.Encode(e)
	if err == nil {
		err = messaging.CheckType(e.Type)
	}
	if err == nil {
		err = carriable(h)
	}
	if err != nil {
		return fmt.Errorf("nats: publish: %w", err)
	}
	msg := &natsgo.Msg{Subject: b.subject(e.Type), Header: natsgo.Header(h), Data: body}
	msg.Header.Set(jetstream.MsgIDHeader, msgID(e))
	if _, err := b.js.PublishMsg(ctx, msg); err != nil {
		return fmt.Errorf("nats: publish %s: %w", e.ID, err)
	}
	return nil
}

// msgID is e's deduplication key: its source and id, which together
// identify a CloudEvents event. The source is length-prefixed, so no source
// and id pair can spell another's key.
func msgID(e event.Event) string {
	return strconv.Itoa(len(e.Source)) + ":" + e.Source + e.ID
}

// carriable fails on a header value the NATS protocol cannot carry.
func carriable(h event.Header) error {
	for name, vs := range h {
		for _, v := range vs {
			if strings.ContainsAny(v, "\r\n") {
				return fmt.Errorf("header %s: a value must not contain CR or LF", name)
			}
		}
	}
	return nil
}

func (b *Broker) subject(typ string) string { return b.cfg.Prefix + "." + typ }

// Subscribe validates sub and returns a source that binds its durable
// consumer when it starts receiving.
func (b *Broker) Subscribe(sub messaging.Subscription) (reactor.Source[event.Event], error) {
	if err := sub.Validate(); err != nil {
		return nil, err
	}
	return &source{b: b, sub: sub, cfg: b.consumerConfig(sub)}, nil
}

// consumerConfig is the durable consumer sub describes. It is derived the
// same way for every member, so members of a Name bind one consumer, and a
// different subscription under the Name fails to bind.
func (b *Broker) consumerConfig(sub messaging.Subscription) jetstream.ConsumerConfig {
	wait := sub.AckWait
	if wait == 0 {
		wait = DefaultAckWait
	}
	maxDeliver := sub.MaxDeliver
	if maxDeliver == 0 {
		maxDeliver = -1
	}
	cfg := jetstream.ConsumerConfig{
		Durable:       sub.Name,
		DeliverPolicy: jetstream.DeliverAllPolicy,
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       wait + AckMargin,
		MaxDeliver:    maxDeliver,
	}
	if len(sub.Types) > 0 {
		types := append([]string(nil), sub.Types...)
		slices.Sort(types)
		types = slices.Compact(types)
		for _, t := range types {
			cfg.FilterSubjects = append(cfg.FilterSubjects, b.subject(t))
		}
	}
	return cfg
}
