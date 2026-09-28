package messaging

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/JaimeStill/spike-messaging/core/event"
	"github.com/JaimeStill/spike-messaging/core/reactor"
)

// Publisher publishes an event. Publish returns once the broker holds the
// event, so a delivery can outlive the publisher.
//
// Publish rejects an event that fails [event.Event.Validate] or whose type
// breaks [CheckType]. The broker deduplicates on the event's source and id,
// which together identify a CloudEvents event: an event whose source and id
// the broker has seen within its deduplication window is accepted and
// dropped, so a publisher that retries, such as the outbox's relay, delivers
// the event once, while another source's event that reuses the id still
// delivers.
type Publisher interface {
	Publish(ctx context.Context, e event.Event) error
}

// Broker publishes events and subscribes reactors to them.
type Broker interface {
	Publisher
	// Subscribe validates sub and returns a source of its events: a member
	// of the durable consumer sub.Name names.
	//
	// The first subscription under a Name creates its consumer, which starts
	// at the beginning of the stream, so it receives every event, including
	// those published before it subscribed. A later subscription under the
	// same Name binds that consumer and must match its configuration; a
	// mismatch fails Subscribe, or the source's Receive when the provider
	// binds there.
	//
	// A member claims one delivery at a time, and the next only once the
	// handler's outcome is settled, so members of one Name share the work
	// rather than one member taking a batch. The outcome is the handler's
	// return: nil acknowledges the delivery; an error redelivers it after
	// sub.RetryDelay, until sub.MaxDeliver deliveries have been made; an
	// error marked by [event.Permanent] terminates it, so it is never
	// delivered again. A handler error never ends the source.
	Subscribe(sub Subscription) (reactor.Source[event.Event], error)
}

// Subscription describes a durable consumer of events.
type Subscription struct {
	// Name is the durable consumer, and so its delivery group: sources
	// subscribed under the same Name share one position and split the work.
	// It is a token: no whitespace, '.', '*', '>', '/', or '\'.
	Name string
	// Types filters on the event's type; empty matches every type. Each
	// entry must pass [CheckType].
	Types []string
	// MaxDeliver bounds the deliveries of one event; 0 is unlimited.
	MaxDeliver int
	// AckWait is the handler's deadline for each delivery, and so the
	// deadline on its context. When it passes, the event is redelivered and
	// the late outcome is ignored. 0 is the provider's default.
	AckWait time.Duration
	// RetryDelay is how long an event whose handler returned an error waits
	// before it is redelivered; 0 redelivers it at once.
	RetryDelay time.Duration
}

// Validate reports every way sub is unusable.
func (sub Subscription) Validate() error {
	var errs []error
	switch {
	case sub.Name == "":
		errs = append(errs, errors.New("name is required"))
	case !IsToken(sub.Name):
		errs = append(errs, fmt.Errorf("name %q must not contain whitespace, '.', '*', '>', '/', or '\\'", sub.Name))
	}
	for _, t := range sub.Types {
		if err := CheckType(t); err != nil {
			errs = append(errs, fmt.Errorf("types: %w", err))
		}
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

// IsToken reports whether s can name a durable consumer or a stream: it is
// non-empty, with no whitespace, control character, '.', '*', '>', '/', or
// '\', the characters a JetStream name may not hold.
func IsToken(s string) bool {
	return s != "" && !strings.ContainsFunc(s, func(r rune) bool {
		return strings.ContainsRune(".*>/\\", r) || unicode.IsSpace(r) || unicode.IsControl(r)
	})
}

// CheckType reports whether t is an event type a broker can route: one or
// more tokens separated by '.', none of them empty, with no whitespace, '*',
// or '>'. The rule is a subject's, so a provider can route on the type
// itself, and it admits the reverse-DNS types CloudEvents recommends, such as
// "lab.grant.approved".
func CheckType(t string) error {
	if t == "" {
		return errors.New("type: an empty type matches nothing")
	}
	for tok := range strings.SplitSeq(t, ".") {
		if tok == "" {
			return fmt.Errorf("type %q: an empty token", t)
		}
		if strings.ContainsFunc(tok, func(r rune) bool { return r == '*' || r == '>' || unicode.IsSpace(r) || unicode.IsControl(r) }) {
			return fmt.Errorf("type %q must not contain whitespace, '*', or '>'", t)
		}
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
