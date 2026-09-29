// Package decide decides what command directs a faction's elements to do:
// for each live element, the target it heads for and the rule that chose
// it.
//
// The package holds command's own reading of the public ruleset. Its [Map]
// decodes the map the exercise.started event carries, and its [Assessment]
// the intelligence.assessment.issued event, because the services share no
// Go types. It is pure: it performs no I/O, and the domain calls it inside a
// command.
//
// [Decide] applies three rules to each element, in order:
//
//  1. engage: a force heads for the nearest known contact weaker than
//     itself within [EngageRange] steps, at the cell it was last seen in;
//  2. secure: otherwise an element heads for the nearest objective its
//     faction does not hold that none of its other elements is heading
//     for;
//  3. hold: otherwise it stands where it is.
//
// Distance is path distance: the fewest steps between two cells, found by
// breadth-first search over the steps an element can take. One step enters
// an orthogonally adjacent cell of the same sector that is inside the grid
// and not an obstacle, or, from a gate's cell, traverses the link to the
// linked gate's cell. A target no step sequence reaches is never chosen.
//
// An objective is held by the faction only when the assessment knows it
// is: an objective no element has seen is unheld, whatever its holder
// field says. A held belief can be stale, since the assessment keeps an
// objective as it was last seen, so an objective the faction believes it
// holds is not secured until an element sees it again.
//
// An element keeps the objective it was securing while that objective is
// still one to secure and it can still reach it, so a faction's targets do
// not trade places as its elements move. Those standing targets are
// claimed before any element picks a new one.
package decide
