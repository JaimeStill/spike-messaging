package nats

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	natsgo "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/JaimeStill/spike-messaging/core/event"
	"github.com/JaimeStill/spike-messaging/core/reactor"
	"github.com/JaimeStill/spike-messaging/messaging"
)

const (
	// AckMargin is how much longer the consumer waits for an outcome than
	// the handler's deadline, so an outcome settled in time reaches the
	// server before it redelivers, on a link whose round trip is well under
	// the margin. On a slower link a settled event can be delivered again,
	// never lost. The margin is part of the consumer's configuration, so a
	// release that changes it cannot bind the durables an earlier one made.
	AckMargin = 250 * time.Millisecond
)

// ErrNotStarted is the failure of a call that needs the connection, made
// before [Broker.Start].
var ErrNotStarted = errors.New("nats: broker not started")

// ErrStarted is the failure of a second [Broker.Start].
var ErrStarted = errors.New("nats: broker already started")

// Broker is a [messaging.Broker] on a JetStream stream.
type Broker struct {
	cfg      Config
	starting atomic.Bool
	conn     atomic.Pointer[conn]
}

// conn is the connection a started broker owns, and its JetStream context.
type conn struct {
	nc *natsgo.Conn
	js jetstream.JetStream
}

var _ messaging.Broker = (*Broker)(nil)

// New checks cfg and returns a broker on it, unconnected. It does no I/O.
func New(cfg Config) (*Broker, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Broker{cfg: cfg}, nil
}

// Start connects to the server, waiting until ctx's deadline or 10s, and
// provisions the stream, creating it or updating it to the broker's
// configuration. Once up, the connection reconnects without limit. A
// failed Start closes what it opened, and the broker can start again. A
// broker starts once: a Start while another is in progress or has
// succeeded fails with [ErrStarted] and opens nothing.
func (b *Broker) Start(ctx context.Context) (err error) {
	if !b.starting.CompareAndSwap(false, true) {
		return ErrStarted
	}
	defer func() {
		if err != nil {
			b.starting.Store(false)
		}
	}()
	timeout := 10 * time.Second
	if d, ok := ctx.Deadline(); ok {
		timeout = time.Until(d)
	}
	nc, err := natsgo.Connect(b.cfg.URL, natsgo.Name(b.cfg.Name), natsgo.Timeout(timeout), natsgo.MaxReconnects(-1))
	if err != nil {
		return fmt.Errorf("nats: connect %s: %w", b.cfg.URL, err)
	}
	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return fmt.Errorf("nats: %w", err)
	}
	_, err = js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:       b.cfg.Stream,
		Subjects:   []string{b.cfg.Prefix + ".>"},
		Storage:    jetstream.FileStorage,
		Retention:  jetstream.LimitsPolicy,
		Duplicates: b.cfg.window(),
		MaxAge:     b.cfg.MaxAge.Duration(),
	})
	if err != nil {
		nc.Close()
		return fmt.Errorf("nats: provision stream %s: %w", b.cfg.Stream, err)
	}
	b.conn.Store(&conn{nc: nc, js: js})
	return nil
}

// Conn is the native handle, for a use beyond the standard tier, such as
// request and reply: the started broker's connection, or nil before Start.
func (b *Broker) Conn() *natsgo.Conn {
	if c := b.conn.Load(); c != nil {
		return c.nc
	}
	return nil
}

// Ready reports whether the broker is started and its connection is up.
func (b *Broker) Ready() bool {
	c := b.conn.Load()
	return c != nil && c.nc.IsConnected()
}

// Shutdown drains the connection: its subscriptions stop, pending
// publishes and acknowledgements flush, and it closes. It returns once the
// connection is closed, at once for a broker that never started. When ctx
// ends first, it closes the connection without finishing the drain and
// returns ctx's error.
func (b *Broker) Shutdown(ctx context.Context) error {
	c := b.conn.Load()
	if c == nil {
		return nil
	}
	// A connection that is reconnecting cannot drain, and Drain closes it.
	if err := c.nc.Drain(); err != nil && !errors.Is(err, natsgo.ErrConnectionClosed) && !errors.Is(err, natsgo.ErrConnectionReconnecting) {
		c.nc.Close()
		return fmt.Errorf("nats: drain: %w", err)
	}
	t := time.NewTicker(10 * time.Millisecond)
	defer t.Stop()
	for !c.nc.IsClosed() {
		select {
		case <-ctx.Done():
			c.nc.Close()
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
	h, body, err := messaging.Encode(e)
	if err == nil {
		err = carriable(h)
	}
	if err != nil {
		return fmt.Errorf("nats: publish: %w", err)
	}
	c := b.conn.Load()
	if c == nil {
		return ErrNotStarted
	}
	msg := &natsgo.Msg{Subject: b.subject(e.Type), Header: natsgo.Header(h), Data: body}
	msg.Header.Set(jetstream.MsgIDHeader, msgID(e))
	if _, err := c.js.PublishMsg(ctx, msg); err != nil {
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
	sub = sub.Normalize()
	maxDeliver := sub.MaxDeliver
	if maxDeliver == 0 {
		maxDeliver = -1
	}
	cfg := jetstream.ConsumerConfig{
		Durable:       sub.Name,
		DeliverPolicy: jetstream.DeliverAllPolicy,
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       sub.AckWait + AckMargin,
		MaxDeliver:    maxDeliver,
	}
	for _, t := range sub.Types {
		cfg.FilterSubjects = append(cfg.FilterSubjects, b.subject(t))
	}
	return cfg
}
