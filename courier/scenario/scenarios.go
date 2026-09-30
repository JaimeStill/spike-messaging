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

// Joins builds a broker on an existing stream, the one the exercise
// services share, with the release that frees the broker and the durable
// consumers the run left on the stream. Unlike a scratch broker's, the
// release leaves the stream and its events in place. The release may be nil.
type Joins func(stream, prefix string, maxAge time.Duration) (b messaging.Broker, release func() error, err error)

// Dependencies is what the composition root supplies the scenarios. Each
// func() []Need is called when a run starts or the listing is written, not
// when the scenarios are built, so it can follow the parsed flags; any of
// them may be nil for nothing needed.
type Dependencies struct {
	// Brokers builds each run's broker, and BrokerNeeds is what it requires.
	Brokers     Brokers
	BrokerNeeds func() []Need
	// Outboxes builds the outbox scenario's store, which also requires
	// OutboxNeeds, checked after the broker's needs.
	Outboxes    Outboxes
	OutboxNeeds func() []Need
	// Exchanges builds the request scenario's exchange, which requires
	// RequestNeeds in place of the broker's needs.
	Exchanges    Exchanges
	RequestNeeds func() []Need
	// Joins builds the broker of the scenarios that join the exercise
	// services' stream (directives, assessments, theater, and
	// theater-check), which requires JoinNeeds in place of the broker's
	// needs.
	Joins     Joins
	JoinNeeds func() []Need
}

// Scenarios returns every scenario in presentation order, built on d. The
// every scenario builds no broker, so it needs nothing.
func Scenarios(d Dependencies) []Scenario {
	return []Scenario{
		everyScenario(nil),
		groupScenario(d.Brokers, d.BrokerNeeds),
		retryScenario(d.Brokers, d.BrokerNeeds),
		permanentScenario(d.Brokers, d.BrokerNeeds),
		drainScenario(d.Brokers, d.BrokerNeeds),
		outboxScenario(d.Brokers, d.Outboxes, concatNeeds(d.BrokerNeeds, d.OutboxNeeds)),
		requestScenario(d.Exchanges, d.RequestNeeds),
		directivesScenario(d.Joins, d.JoinNeeds),
		assessmentsScenario(d.Joins, d.JoinNeeds),
		theaterScenario(d.Joins, d.JoinNeeds),
		theaterCheckScenario(d.Joins, d.JoinNeeds),
	}
}

// concatNeeds returns the needs of each list in order, calling each when a
// run starts; a nil list contributes nothing.
func concatNeeds(lists ...func() []Need) func() []Need {
	return func() []Need {
		var all []Need
		for _, l := range lists {
			if l != nil {
				all = slices.Concat(all, l())
			}
		}
		return all
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

// numbered is the event n the broker scenarios publish, of the tick type the
// outbox scenario raises.
func numbered(n int) event.Event {
	return event.Event{ID: strconv.Itoa(n), Source: "/courier", Type: tickKind.Type(), Time: time.Now()}
}

// signal is a channel closed once, however many times fire is called.
type signal struct {
	once sync.Once
	ch   chan struct{}
}

func newSignal() *signal { return &signal{ch: make(chan struct{})} }

func (s *signal) fire() { s.once.Do(func() { close(s.ch) }) }

// isFired reports whether the signal has fired.
func (s *signal) isFired() bool {
	select {
	case <-s.ch:
		return true
	default:
		return false
	}
}

// sleep waits d, or until ctx ends, and reports whether it waited in full.
func sleep(ctx context.Context, d time.Duration) bool {
	select {
	case <-time.After(d):
		return true
	case <-ctx.Done():
		return false
	}
}

// lease holds the release of what one run acquired: its broker, or the
// request scenario's exchange.
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

// close calls the release, if one was acquired. It is safe to call more
// than once.
func (l *lease) close() error {
	release := l.release
	l.release = nil
	if release == nil {
		return nil
	}
	return release()
}

// afterDrain is a run's cleanup: it drains the coordinator, then calls the
// lease's release, so no reactor still holds what it frees.
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
