// Package route plans how operations maneuvers a faction's elements: the
// steps that carry each element toward its target in one round.
//
// The package holds operations' own reading of the public ruleset. Its
// [Map] decodes the map the exercise.started event carries, and its
// [Element] the elements an exercise.round.observed event reports, because
// the services share no Go types. It is pure: it performs no I/O, and the
// domain calls it inside a command.
//
// One step enters an orthogonally adjacent cell of the same sector that is
// inside the grid and not an obstacle, or, from a gate's cell, traverses the
// link to the linked gate's cell; entering a gate and traversing it are
// separate steps. [Plan] finds a shortest path to each target by
// breadth-first search over those steps, and orders the element along it up
// to its kind's moves per round.
package route
