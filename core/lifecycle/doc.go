// Package lifecycle registers a component on go-core's lifecycle
// coordinator in one call.
//
// A [Component] has the three methods every piece of infrastructure joins
// the lifecycle through, Start, Shutdown, and Ready, and Err, the channel
// that reports a failure after Start. A reactor is one. [Register] adds a
// component at the stage the caller names, with its readiness check, and
// monitors its Err, which a lifecycle.Service alone cannot carry. The stage
// stays at the call site, because it is the process's dependency order,
// which a library cannot know.
//
// The package is named for its intended home: it is the spike's evidence
// for go-core's lifecycle gaining a component interface and a registration
// by name and stage. It builds against published go-core, which it does not
// change.
package lifecycle
