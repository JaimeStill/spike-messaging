package logging

import (
	"context"
	"log/slog"
	"time"
)

// Throttle logs the outcome of a repeating operation: the first failure at
// once, then at most one line per interval while failures persist, counting
// the ones it held back, and one line when the operation succeeds again.
// One goroutine reports to it, as a reactor's handler does, so it takes no
// lock.
type Throttle struct {
	logger     *slog.Logger
	every      time.Duration
	now        func() time.Time
	failing    bool
	last       time.Time
	suppressed int
}

// NewThrottle returns a throttle that logs to logger at most once per every
// while a failure persists.
func NewThrottle(logger *slog.Logger, every time.Duration) *Throttle {
	return &Throttle{logger: logger, every: every, now: time.Now}
}

// Report records one outcome of the operation named msg: err, or nil for a
// success.
func (t *Throttle) Report(ctx context.Context, msg string, err error) {
	now := t.now()
	switch {
	case err == nil && t.failing:
		t.logger.InfoContext(ctx, msg+" recovered", "suppressed", t.suppressed)
		t.failing, t.suppressed = false, 0
	case err == nil:
	case !t.failing || now.Sub(t.last) >= t.every:
		t.logger.ErrorContext(ctx, msg, "error", err, "suppressed", t.suppressed)
		t.failing, t.last, t.suppressed = true, now, 0
	default:
		t.suppressed++
	}
}
