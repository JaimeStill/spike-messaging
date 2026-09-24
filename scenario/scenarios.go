package scenario

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/JaimeStill/spike-messaging/core/event"
	"github.com/JaimeStill/spike-messaging/messaging"
)

// Brokers builds a fresh broker for one scenario run.
type Brokers func() (messaging.Broker, error)

// Scenarios returns every scenario in presentation order. Each run builds its
// broker from brokers and checks needs first.
func Scenarios(brokers Brokers, needs []Need) []Scenario {
	return []Scenario{
		everyScenario(needs),
		groupScenario(brokers, needs),
		retryScenario(brokers, needs),
		permanentScenario(brokers, needs),
		drainScenario(brokers, needs),
	}
}

// The drain timeout and grace of the scenarios that do not demonstrate the
// drain itself.
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

// publish publishes e and notes it.
func publish(ctx context.Context, b messaging.Broker, rep *Reporter, e event.Event) error {
	if err := b.Publish(ctx, e); err != nil {
		return fmt.Errorf("publish %s: %w", e.ID, err)
	}
	rep.Note("published event %s", e.ID)
	return nil
}
