package scenario

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/JaimeStill/spike-messaging/core/event"
	"github.com/JaimeStill/spike-messaging/messaging"
)

// Brokers builds a fresh broker for one scenario run, with the release that
// frees what the broker holds, such as a scratch stream. A scenario calls
// the release from its cleanup, once its coordinator has drained. The
// release may be nil.
type Brokers func() (b messaging.Broker, release func() error, err error)

// Scenarios returns every scenario in presentation order. Each run checks
// what needs returns first, and builds its broker from brokers; needs is
// called then, not here, so it can follow the parsed flags, and may be nil.
// The outbox scenario also builds its store from outboxes, and checks
// outboxNeeds after needs.
func Scenarios(brokers Brokers, needs func() []Need, outboxes Outboxes, outboxNeeds []Need) []Scenario {
	return []Scenario{
		everyScenario(needs),
		groupScenario(brokers, needs),
		retryScenario(brokers, needs),
		permanentScenario(brokers, needs),
		drainScenario(brokers, needs),
		outboxScenario(brokers, outboxes, func() []Need {
			if needs == nil {
				return outboxNeeds
			}
			return slices.Concat(needs(), outboxNeeds)
		}),
	}
}

// defaultDrain and defaultGrace are the drain timeout and grace used by the
// scenarios that do not themselves demonstrate the drain.
const (
	defaultDrain = 5 * time.Second
	defaultGrace = 4 * time.Second
)

// group is the subscription the worker scenarios share.
const group = "workers"

// graceBelowDrain is the usage rule every scenario with a grace enforces:
// the coordinator drops a reactor's report once its own deadline passes.
func graceBelowDrain(grace, drain time.Duration) error {
	if grace >= drain {
		return errors.New("--grace must be below --drain, or the coordinator's timeout hides the reactor's report")
	}
	return nil
}

func numbered(n int) event.Event {
	return event.Event{ID: strconv.Itoa(n), Source: "/courier", Type: "lab.demo.tick", Time: time.Now()}
}

// signal is a channel closed once, however many times fire is called.
type signal struct {
	once sync.Once
	ch   chan struct{}
}

func newSignal() *signal { return &signal{ch: make(chan struct{})} }

func (s *signal) fire() { s.once.Do(func() { close(s.ch) }) }

// sleep waits d, or until ctx ends, and reports whether it waited in full.
func sleep(ctx context.Context, d time.Duration) bool {
	select {
	case <-time.After(d):
		return true
	case <-ctx.Done():
		return false
	}
}

// lease holds one run's broker and its release.
type lease struct {
	release func() error
}

// acquire builds a broker from brokers and keeps its release for close.
func (l *lease) acquire(brokers Brokers) (messaging.Broker, error) {
	b, release, err := brokers()
	if err != nil {
		return nil, err
	}
	l.release = release
	return b, nil
}

// close releases the broker, if one was acquired. It is safe to call more
// than once.
func (l *lease) close() error {
	release := l.release
	l.release = nil
	if release == nil {
		return nil
	}
	return release()
}

// afterDrain is a run's cleanup: it drains the coordinator, then releases
// the broker, so no reactor still holds the broker when it goes.
func afterDrain(c *coordinator, l *lease) func() error {
	return func() error { return errors.Join(c.cleanup(), l.close()) }
}

// publish publishes e and notes it.
func publish(ctx context.Context, b messaging.Broker, rep *Reporter, e event.Event) error {
	if err := b.Publish(ctx, e); err != nil {
		return fmt.Errorf("publish %s: %w", e.ID, err)
	}
	rep.Note("published event %s", e.ID)
	return nil
}
