// Package reactor joins one source of occurrences to one function for the
// process lifetime.
//
// A [Source] delivers occurrences: a subscription, an interval ([Every]),
// or a schedule. A [Reactor] runs one source into one [Func] and is a
// lifecycle component, with the Start, Shutdown, and Ready methods other
// infrastructure exposes, plus [Reactor.Err] for a failure while running. The
// package knows nothing of messaging or of the coordinator; the composition
// root registers a reactor at the stage it chooses and monitors its Err.
//
// # Drain
//
// Start detaches the reactor from the context it is given, so cancelling the
// run context at a signal does not interrupt handling before the reactor's
// stage drains. Shutdown drains in two phases: it stops the source from
// taking new occurrences and waits for the handling in flight, then, once
// the [Grace] period passes, cancels the handlers' contexts and waits for
// them to unwind. Without Grace, a handler's context is cancelled only when
// Shutdown's own context ends, which is the drain deadline.
package reactor
