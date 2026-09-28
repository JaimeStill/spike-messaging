package event

import "errors"

// Queue holds the events one command raises, in the order it raises them.
// [Recorder.Emit] gives each run of a command a fresh queue and emits it
// when the command succeeds; a command never emits a queue itself.
type Queue struct {
	events []Event
	errs   []error
}

// Events returns a copy of the queued events, unstamped, so a test can read
// what a command raised.
func (q *Queue) Events() []Event {
	return append([]Event(nil), q.events...)
}

// Err returns every failure a raise held, or nil.
func (q *Queue) Err() error { return errors.Join(q.errs...) }

func (q *Queue) fail(err error) { q.errs = append(q.errs, err) }
