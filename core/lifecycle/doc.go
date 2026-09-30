// Package lifecycle registers a component on go-core's lifecycle
// coordinator in one call.
//
// A [Component] has the methods Start, Shutdown, and Ready, through which
// infrastructure such as a database or a broker joins the lifecycle. A
// [Monitored] component also has Err, a channel that reports a failure after
// Start; a reactor is one. [Register] adds a component at the stage the
// caller names, with the component as its readiness check, and monitors the
// Err of a monitored component, which a lifecycle.Service cannot carry. The
// stage stays at the call site because it encodes the process's dependency
// order, which a library cannot know.
//
// The package sits in a directory named for its intended home. It is the
// spike's evidence for adding a component interface and registration by name
// and stage to go-core's lifecycle. It builds against published go-core and
// does not change it.
package lifecycle
