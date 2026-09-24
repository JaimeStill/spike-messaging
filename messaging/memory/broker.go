package memory

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/JaimeStill/spike-messaging/core/event"
	"github.com/JaimeStill/spike-messaging/core/reactor"
	"github.com/JaimeStill/spike-messaging/messaging"
)

// DefaultAckWait is the AckWait of a subscription that sets none.
const DefaultAckWait = 30 * time.Second

// Broker is an in-memory [messaging.Broker]. Its zero value is not usable;
// call [New].
//
// One mutex guards the log and every consumer, so the consumer's methods
// never lock; the broker and the sources take mu around them.
type Broker struct {
	mu        sync.Mutex
	log       []message // an event's sequence is its index
	consumers map[string]*consumer
}

type message struct {
	typ    string // kept beside the header, so the type filter need not decode
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

// Subscribe returns a source on the durable consumer that sub names,
// creating it on first use. Sources under one Name share its position and
// split its work. A later subscription under an existing Name must match
// the first, as binding to a JetStream durable must.
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
		c = newConsumer(sub)
		b.consumers[sub.Name] = c
	} else if !reflect.DeepEqual(c.sub, sub) {
		return nil, fmt.Errorf("memory: subscription %q exists with a different configuration", sub.Name)
	}
	return &source{b: b, c: c}, nil
}
