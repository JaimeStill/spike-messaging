package scenario

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"uuid"

	"github.com/spf13/pflag"

	"github.com/JaimeStill/spike-messaging/core/event"
	corelifecycle "github.com/JaimeStill/spike-messaging/core/lifecycle"
	"github.com/JaimeStill/spike-messaging/core/reactor"
	"github.com/JaimeStill/spike-messaging/messaging"
)

// joinFlags are the flags every scenario that joins the exercise services'
// stream binds: the exercise it follows, the stream and its subject prefix,
// the stream's max_age, and how long it waits.
type joinFlags struct {
	exercise, stream, prefix string
	maxAge, wait             time.Duration
}

// newJoinFlags returns the flags with a max_age default of a day and wait as
// the --wait default.
func newJoinFlags(wait time.Duration) *joinFlags {
	return &joinFlags{maxAge: 24 * time.Hour, wait: wait}
}

// bind binds the flags to fs. The caller supplies the help text of
// --exercise and --wait, which each scenario words for itself. The flags'
// current values are their defaults.
func (j *joinFlags) bind(fs *pflag.FlagSet, exerciseHelp, waitHelp string) {
	fs.StringVar(&j.exercise, "exercise", "", exerciseHelp)
	fs.StringVar(&j.stream, "stream", "exercise", "the stream the exercise services share")
	fs.StringVar(&j.prefix, "prefix", "exercise", "the subject prefix of the services' stream")
	fs.DurationVar(&j.maxAge, "max-age", j.maxAge, "the stream's max_age, as the services configure it: the last broker to provision the stream sets it")
	fs.DurationVar(&j.wait, "wait", j.wait, waitHelp)
}

// validate returns the flags' usage errors: an --exercise that is not an
// exercise ID, each error in more, and a --wait that is not positive.
func (j *joinFlags) validate(more ...error) error {
	errs := append([]error{j.exerciseErr()}, more...)
	if j.wait <= 0 {
		errs = append(errs, errors.New("--wait must be positive"))
	}
	return errors.Join(errs...)
}

// exerciseErr returns the usage error for an --exercise that is not an
// exercise ID, or nil.
func (j *joinFlags) exerciseErr() error {
	if _, err := uuid.Parse(j.exercise); err != nil {
		return errors.New("--exercise must be an exercise's ID")
	}
	return nil
}

// join joins the stream the flags name through joins, and keeps the
// broker's release for the run's cleanup. It subscribes to types under a
// durable consumer named durable plus a fresh ID, which reads the stream
// from its beginning, registers handle on the subscription as the
// coordinator's watch, and starts the coordinator. It returns the joined
// broker.
func (l *lease) join(ctx context.Context, rep *Reporter, c *coordinator, joins Joins, j *joinFlags, durable string, handle reactor.Func[event.Event], types ...string) (messaging.Broker, error) {
	if joins == nil {
		return nil, errors.New("no stream to join is configured")
	}
	b, release, err := joins(j.stream, j.prefix, j.maxAge)
	if err != nil {
		return nil, err
	}
	l.release = release
	src, err := b.Subscribe(messaging.Subscription{
		Name:  durable + strings.ReplaceAll(uuid.NewV7().String(), "-", ""),
		Types: types,
	})
	if err != nil {
		return nil, err
	}
	corelifecycle.Register(c.lc, "watch", 0, reactor.New(src, handle, reactor.Grace(defaultGrace)))
	if err := c.start(ctx, rep); err != nil {
		return nil, err
	}
	return b, nil
}

// stopWatch returns a join scenario's last step, which signals the
// coordinator's drain and so stops the watch.
func stopWatch(c *coordinator) Step {
	return Step{
		Intent: "Signal the drain: the watch stops receiving",
		Action: func(_ context.Context, rep *Reporter) error { return c.stop(rep) },
	}
}

// conclusion renders an exercise's conclusion as the assessments and
// directives scenarios note it: the round, and the winner and why, or that
// there is none.
func conclusion(v concluded) string {
	if v.Winner == "" {
		return fmt.Sprintf("concluded after round %d with no winner: %s", v.Round, v.Reason)
	}
	return fmt.Sprintf("concluded after round %d: %s wins by %s", v.Round, v.Winner, v.Reason)
}
