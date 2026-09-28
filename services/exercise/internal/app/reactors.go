package app

import (
	"github.com/standards-lab/go-core/lifecycle"
)

// Reactors composes the application's event-driven entry points: components
// that watch a source of occurrences and dispatch each one to a domain
// service call, the inbound counterpart to a route. The template ships it
// empty; which reactors it runs, and what they watch, is the application
// author's decision.
type Reactors struct{}

// newReactors constructs the reactors and registers each on lc. It takes
// infra for the transport connections a reactor owns and dom for the domain
// calls it dispatches to — the two halves a reactor joins. Each reactor owns
// a connection and runs for the process lifetime, so it registers on the
// coordinator the same as an infrastructure service.
func newReactors(
	infra *Infrastructure,
	dom *Domain,
	lc *lifecycle.Coordinator,
) (*Reactors, error) {
	return &Reactors{}, nil
}
