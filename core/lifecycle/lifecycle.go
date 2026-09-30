package lifecycle

import (
	"context"

	golifecycle "github.com/standards-lab/go-core/lifecycle"
)

// Component is what a lifecycle participant offers the coordinator: the
// three lifecycle methods.
type Component interface {
	Start(context.Context) error
	Shutdown(context.Context) error
	Ready() bool
}

// Monitored is a component that can fail after Start, such as a reactor. Err
// reports the failure.
type Monitored interface {
	Component
	Err() <-chan error
}

// Register adds c to lc at stage under name, with c as its readiness check.
// When c is [Monitored], Register also monitors its Err, so a failure after
// Start ends the run.
func Register(lc *golifecycle.Coordinator, name string, stage int, c Component) {
	lc.Add(golifecycle.Service{Name: name, Stage: stage, Start: c.Start, Shutdown: c.Shutdown, Check: c})
	if m, ok := c.(Monitored); ok {
		lc.Monitor(m.Err())
	}
}
