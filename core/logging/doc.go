// Package logging holds the logging the spike's composition roots repeat.
//
// A [Throttle] logs the outcome of an operation that repeats on every tick,
// such as a reactor's pass over due work, without flooding the log while a
// failure persists. It is a stopgap for reporting the errors a reactor
// survives, until an error hook or the observability layer carries them.
// The package is named for its intended home in go-core's logging.
package logging
