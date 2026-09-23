package reactor

import (
	"context"
	"sync/atomic"
	"time"
)

// Every returns a source that delivers the time on every tick of d. The
// handler runs synchronously, so a tick that arrives while it runs is
// dropped. A handler error ends the source. Every panics if d is not
// positive, as time.NewTicker does.
func Every(d time.Duration) Source[time.Time] {
	if d <= 0 {
		panic("reactor: Every needs a positive interval")
	}
	return &every{d: d}
}

type every struct {
	d     time.Duration
	ready atomic.Bool
}

func (s *every) Receive(ctx context.Context, fn Func[time.Time]) error {
	t := time.NewTicker(s.d)
	defer t.Stop()
	s.ready.Store(true)
	defer s.ready.Store(false)
	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-t.C:
			if ctx.Err() != nil {
				return nil
			}
			if err := fn(ctx, now); err != nil {
				return err
			}
		}
	}
}

func (s *every) Ready() bool { return s.ready.Load() }
