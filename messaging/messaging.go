package messaging

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/JaimeStill/spike-messaging/core/event"
	"github.com/JaimeStill/spike-messaging/core/reactor"
)

// Publisher publishes an event. Publish returns once the broker holds the
// event, so a delivery can outlive the publisher.
type Publisher interface {
	Publish(ctx context.Context, e event.Event) error
}

// Broker publishes events and subscribes reactors to them.
type Broker interface {
	Publisher
	// Subscribe validates sub and returns a source of its events. The
	// handler's return is the delivery's outcome: nil acknowledges it; an
	// error redelivers it after sub.RetryDelay, until sub.MaxDeliver
	// deliveries have been made; an error marked by [event.Permanent]
	// terminates it, so it is never delivered again. A handler error never
	// ends the source. A provider sets up the consumer either here or when
	// the source starts receiving.
	Subscribe(sub Subscription) (reactor.Source[event.Event], error)
}

// Subscription describes a durable consumer of events.
type Subscription struct {
	// Name is the durable consumer, and so its delivery group: sources
	// subscribed under the same Name share one position and split the work.
	// It is a token: no whitespace, '.', '*', or '>'.
	Name string
	// Types filters on the event's type; empty matches every type.
	Types []string
	// MaxDeliver bounds the deliveries of one event; 0 is unlimited.
	MaxDeliver int
	// AckWait is the handler's deadline for each delivery, and so the
	// deadline on its context. When it passes, the event is redelivered and
	// the late outcome is ignored. 0 is the provider's default.
	AckWait time.Duration
	// RetryDelay is how long an event whose handler returned an error waits
	// before it is redelivered.
	RetryDelay time.Duration
}

// Validate reports every way sub is unusable.
func (sub Subscription) Validate() error {
	var errs []error
	switch {
	case sub.Name == "":
		errs = append(errs, errors.New("name is required"))
	case strings.ContainsAny(sub.Name, ".*> \t\r\n"):
		errs = append(errs, fmt.Errorf("name %q must not contain whitespace, '.', '*', or '>'", sub.Name))
	}
	if slices.Contains(sub.Types, "") {
		errs = append(errs, errors.New("types: an empty type matches nothing"))
	}
	if sub.MaxDeliver < 0 {
		errs = append(errs, errors.New("max deliver must not be negative"))
	}
	if sub.AckWait < 0 {
		errs = append(errs, errors.New("ack wait must not be negative"))
	}
	if sub.RetryDelay < 0 {
		errs = append(errs, errors.New("retry delay must not be negative"))
	}
	if len(errs) > 0 {
		return fmt.Errorf("messaging: subscription: %w", errors.Join(errs...))
	}
	return nil
}

// Matches reports whether sub's type filter admits an event of type t.
func (sub Subscription) Matches(t string) bool {
	if len(sub.Types) == 0 {
		return true
	}
	return slices.Contains(sub.Types, t)
}
