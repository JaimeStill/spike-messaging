package lifecycle

import (
	"context"

	golifecycle "github.com/standards-lab/go-core/lifecycle"
)

// Component is what a lifecycle participant offers the coordinator: the
// three lifecycle methods, and the channel that reports a failure after
// Start.
type Component interface {
	Start(context.Context) error
	Shutdown(context.Context) error
	Ready() bool
	Err() <-chan error
}

// Register adds c to lc at stage under name, with c as its readiness check,
// and monitors c's Err, so a failure after Start ends the run.
func Register(lc *golifecycle.Coordinator, name string, stage int, c Component) {
	lc.Add(golifecycle.Service{Name: name, Stage: stage, Start: c.Start, Shutdown: c.Shutdown, Check: c})
	lc.Monitor(c.Err())
}
