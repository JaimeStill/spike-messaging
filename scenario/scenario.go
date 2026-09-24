package scenario

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/pflag"
)

// ErrUsage marks an error the invocation caused, such as an invalid flag or
// combination of flags. The composition root maps it to the usage exit code.
var ErrUsage = errors.New("usage")

// Scenario is one narrated capability: what it shows, what it requires, and
// the steps that show it.
type Scenario struct {
	Name    string // the word after "courier scenario"
	Summary string // the line the listing prints
	Needs   []Need
	// Flags binds the scenario's own flags; nil for none.
	Flags func(*pflag.FlagSet)
	// Validate rejects an invalid combination of the parsed flags before
	// anything runs; nil accepts every combination.
	Validate func() error
	// Steps builds the steps of one run, and the cleanup that runs after them
	// however they end. The cleanup may be nil.
	Steps func() (steps []Step, cleanup func() error)
}

// Need is a precondition a scenario checks before its first step.
type Need struct {
	What  string
	Check func(context.Context) error
}

// Step is one beat of the narration: what is about to happen, and the action
// that does it.
type Step struct {
	Intent string
	Action func(context.Context, *Reporter) error
}

// Run validates s's flags, checks every need, then runs its steps in order,
// narrating each intent through r before its action. It stops at the first
// failure, and runs the cleanup either way once the steps have been built.
// A validation failure wraps [ErrUsage].
func Run(ctx context.Context, s Scenario, r *Reporter) (err error) {
	if s.Validate != nil {
		if verr := s.Validate(); verr != nil {
			return fmt.Errorf("%s: %w: %w", s.Name, ErrUsage, verr)
		}
	}
	for _, n := range s.Needs {
		if cerr := n.Check(ctx); cerr != nil {
			return fmt.Errorf("%s: need %s: %w", s.Name, n.What, cerr)
		}
	}
	steps, cleanup := s.Steps()
	if cleanup != nil {
		defer func() {
			if cerr := cleanup(); cerr != nil {
				err = errors.Join(err, fmt.Errorf("%s: cleanup: %w", s.Name, cerr))
			}
		}()
	}
	for i, step := range steps {
		if err := ctx.Err(); err != nil {
			return err
		}
		r.Intent(i+1, len(steps), step.Intent)
		if err := step.Action(ctx, r); err != nil {
			return fmt.Errorf("%s: step %d: %w", s.Name, i+1, err)
		}
	}
	return nil
}
