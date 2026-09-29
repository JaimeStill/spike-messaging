// Package logging holds logging helpers that the spike's composition roots
// would otherwise each repeat.
//
// A [Throttle] logs the outcome of an operation that repeats on every tick,
// such as a reactor's pass over due work, without flooding the log while a
// failure persists. It is a stopgap for reporting the errors a reactor
// survives, until an error hook or the observability layer carries them.
// The package sits in a directory named for its intended home, go-core's
// logging.
package logging
