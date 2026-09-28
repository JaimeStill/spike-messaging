package app

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	natsgo "github.com/nats-io/nats.go"

	"github.com/JaimeStill/spike-messaging/core/event"
	"github.com/JaimeStill/spike-messaging/core/reactor"
	"github.com/JaimeStill/spike-messaging/messaging"
	"github.com/JaimeStill/spike-messaging/messaging/nats"
)

// errNotStarted is the failure of a broker call made before the broker's
// Start, which the coordinator's stage order prevents.
var errNotStarted = errors.New("broker: not started")

// broker is the NATS broker as a lifecycle component at the lowest stage.
// nats.New connects and provisions the stream, so it is I/O and cannot run
// at construction, where the template forbids it; broker defers it to
// Start. Its Subscribe likewise returns a source that binds to the started
// broker when a reactor begins receiving, which the stage order places
// after Start.
type broker struct {
	url  string
	name string
	cfg  nats.Config
	b    atomic.Pointer[nats.Broker]
}

var _ messaging.Broker = (*broker)(nil)

// newBroker checks cfg against the broker's naming rules and returns the
// component, unconnected.
func newBroker(url, name string, cfg nats.Config) (*broker, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &broker{url: url, name: name, cfg: cfg}, nil
}

// Start connects to NATS and provisions the stream. The connection
// reconnects without limit once it is up, so a NATS outage makes the
// broker not ready rather than ending the process.
func (b *broker) Start(ctx context.Context) error {
	timeout := 10 * time.Second
	if d, ok := ctx.Deadline(); ok {
		timeout = time.Until(d)
	}
	nc, err := natsgo.Connect(b.url, natsgo.Name(b.name), natsgo.Timeout(timeout), natsgo.MaxReconnects(-1))
	if err != nil {
		return fmt.Errorf("broker: connect %s: %w", b.url, err)
	}
	nb, err := nats.New(ctx, nc, b.cfg)
	if err != nil {
		nc.Close()
		return fmt.Errorf("broker: %w", err)
	}
	b.b.Store(nb)
	return nil
}

// Shutdown drains the connection, after every reactor has drained.
func (b *broker) Shutdown(ctx context.Context) error {
	if nb := b.b.Load(); nb != nil {
		return nb.Shutdown(ctx)
	}
	return nil
}

// Ready reports whether the broker is started and its connection is up.
func (b *broker) Ready() bool {
	nb := b.b.Load()
	return nb != nil && nb.Ready()
}

// Publish publishes e on the started broker.
func (b *broker) Publish(ctx context.Context, e event.Event) error {
	nb := b.b.Load()
	if nb == nil {
		return errNotStarted
	}
	return nb.Publish(ctx, e)
}

// Subscribe checks sub and returns a source that subscribes on the started
// broker when it begins receiving.
func (b *broker) Subscribe(sub messaging.Subscription) (reactor.Source[event.Event], error) {
	if err := sub.Validate(); err != nil {
		return nil, err
	}
	return &subscription{b: b, sub: sub}, nil
}

// subscription is a source bound to its broker at Receive.
type subscription struct {
	b   *broker
	sub messaging.Subscription
	src atomic.Pointer[reactor.Source[event.Event]]
}

func (s *subscription) Receive(ctx context.Context, fn reactor.Func[event.Event]) error {
	nb := s.b.b.Load()
	if nb == nil {
		return errNotStarted
	}
	src, err := nb.Subscribe(s.sub)
	if err != nil {
		return err
	}
	s.src.Store(&src)
	return src.Receive(ctx, fn)
}

func (s *subscription) Ready() bool {
	src := s.src.Load()
	return src != nil && (*src).Ready()
}
