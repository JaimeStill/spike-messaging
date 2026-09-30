package operations

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/standards-lab/sqlate"

	"github.com/JaimeStill/spike-messaging/services/operations/domain/operations/route"
)

// ErrValidation classifies a command input that the domain rejects because
// of its own fields. The returned error wraps it with every reason found.
var ErrValidation = errors.New("invalid command")

// ErrNotFound reports an exercise for which the service has opened no
// operation. It wraps sql.ErrNoRows, so a caller that tests for either
// finds it.
var ErrNotFound = fmt.Errorf("operation not found: %w", sql.ErrNoRows)

// ErrNotOpen reports an input for a faction's operation that is not open
// yet, as when a round's observation is handled before the start that opens
// it. It is not permanent: the broker redelivers the input, and the start
// has opened the operation by then.
var ErrNotOpen = errors.New("the operation is not open yet")

// Status is where a faction's operation is in its life.
type Status string

// The statuses of an operation.
const (
	// StatusOpen is an operation that maneuvers its faction's elements.
	StatusOpen Status = "open"
	// StatusClosed is an operation whose exercise concluded.
	StatusClosed Status = "closed"
)

// Operation is one faction's operation in an exercise, as the query shows
// it. It holds the faction's elements as its last observation left them;
// each element's standing, the target and rule its directive set; LastRound,
// the last observed round it issued orders from, -1 before the first; and
// DirectiveSeq, the sequence of the last applied directive, 0 before the
// first. Its JSON shows the standing as two maps keyed by element, targets
// and rules.
type Operation struct {
	Exercise     string                    `json:"exercise"`
	Faction      string                    `json:"faction"`
	Status       Status                    `json:"status"`
	LastRound    int                       `json:"last_round"`
	DirectiveSeq int                       `json:"directive_sequence"`
	Elements     []route.Element           `json:"elements"`
	Standing     map[string]route.Standing `json:"-"`
	UpdatedAt    time.Time                 `json:"updated_at"`
	plan         route.Map
	limit        int
}

// MarshalJSON shows the operation with its standing split into targets and
// rules, each keyed by element.
func (op Operation) MarshalJSON() ([]byte, error) {
	type fields Operation
	targets, rules := split(op.Standing)
	return json.Marshal(struct {
		fields
		Targets map[string]route.Location `json:"targets"`
		Rules   map[string]string         `json:"rules"`
	}{fields(op), targets, rules})
}

// split returns the targets and the rules of standing, each keyed by
// element, as the operation's JSON and its columns keep them.
func split(standing map[string]route.Standing) (map[string]route.Location, map[string]string) {
	targets := make(map[string]route.Location, len(standing))
	rules := make(map[string]string, len(standing))
	for id, st := range standing {
		targets[id], rules[id] = st.Target, st.Rule
	}
	return targets, rules
}

// Open is the open command's input, operations' reading of an
// exercise.started event: the exercise, its public map, its two factions,
// and its round limit.
type Open struct {
	Exercise   string    `json:"exercise"`
	Map        route.Map `json:"map"`
	Factions   [2]string `json:"factions"`
	RoundLimit int       `json:"round_limit"`
}

// Validate reports every way c is unusable, wrapping [ErrValidation].
func (c Open) Validate() error {
	var errs []error
	errs = append(errs, checkExercise(c.Exercise))
	for _, f := range c.Factions {
		if f == "" {
			errs = append(errs, errors.New("a faction is empty"))
		}
	}
	if c.Factions[0] == c.Factions[1] {
		errs = append(errs, errors.New("the factions are the same"))
	}
	if c.RoundLimit < 1 {
		errs = append(errs, fmt.Errorf("round limit %d is below 1", c.RoundLimit))
	}
	return invalid(errs)
}

// Directive is one element's directive: the rule command decided it by,
// such as secure or retreat, and the target it heads for, or no target,
// which holds it where it stands. command's decide package names the rules.
type Directive struct {
	Element string          `json:"element"`
	Rule    string          `json:"rule"`
	Target  *route.Location `json:"target"`
}

// Assign is the assign command's input, operations' reading of a
// command.directive.issued event: a faction's directives, decided on the
// assessment of an observed round. Sequence numbers a faction's directives
// in an exercise in the order command decided them, from 1, so a higher one
// is newer whatever round it names.
type Assign struct {
	Exercise   string      `json:"exercise"`
	Faction    string      `json:"faction"`
	Round      int         `json:"round"`
	Sequence   int         `json:"sequence"`
	Directives []Directive `json:"directives"`
}

// Validate reports every way c is unusable, wrapping [ErrValidation].
func (c Assign) Validate() error {
	errs := []error{checkExercise(c.Exercise), checkFaction(c.Faction), checkRound(c.Round), checkCounter("sequence", c.Sequence)}
	for i, d := range c.Directives {
		if d.Element == "" {
			errs = append(errs, fmt.Errorf("directive %d: the element is empty", i))
		}
	}
	return invalid(errs)
}

// Maneuver is the maneuver command's input, operations' reading of an
// exercise.round.observed event: a faction's own elements after a round.
type Maneuver struct {
	Exercise string          `json:"exercise"`
	Faction  string          `json:"faction"`
	Round    int             `json:"round"`
	Own      []route.Element `json:"own"`
}

// Validate reports every way c is unusable, wrapping [ErrValidation].
func (c Maneuver) Validate() error {
	return invalid([]error{checkExercise(c.Exercise), checkFaction(c.Faction), checkRound(c.Round)})
}

// Close is the close command's input, operations' reading of an
// exercise.concluded event.
type Close struct {
	Exercise string `json:"exercise"`
}

// Validate reports every way c is unusable, wrapping [ErrValidation].
func (c Close) Validate() error {
	return invalid([]error{checkExercise(c.Exercise)})
}

// Claim is an idempotency claim that a command runs in its transaction
// before it does anything else. It reports whether this is the first time
// the command's input was handled. On false the command changes nothing and
// succeeds. The consuming reactor binds a Claim over its inbox and the
// event it handles. A caller without an inbox, such as a test, passes a nil
// Claim, which claims nothing. It is an alias, so the claim that
// messaging's Consume supplies to a consumer is a Claim.
type Claim = func(ctx context.Context, tx *sqlate.Tx) (first bool, err error)

func checkExercise(id string) error {
	if _, err := uuid.Parse(id); err != nil {
		return fmt.Errorf("exercise %q is not a UUID", id)
	}
	return nil
}

func checkFaction(f string) error {
	if f == "" {
		return errors.New("the faction is empty")
	}
	return nil
}

func checkRound(r int) error {
	if r < 0 {
		return fmt.Errorf("round %d is negative", r)
	}
	return nil
}

// checkCounter checks n, a producer's counter, which counts from 1.
func checkCounter(name string, n int) error {
	if n < 1 {
		return fmt.Errorf("%s %d is not positive", name, n)
	}
	return nil
}

// invalid joins the reasons found, or returns nil for none.
func invalid(errs []error) error {
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("%w: %w", ErrValidation, err)
	}
	return nil
}
