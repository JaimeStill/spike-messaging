// Package rules is the world model of an exercise of sector dominance, and
// the resolution of its rounds. Two factions move elements across the
// sectors of a public [Map], contest its objectives, and see only what their
// elements can see.
//
// The package is pure: its functions take and return values, and do no I/O,
// read no clock, and draw no random numbers, so a round resolves the same
// way every time it is replayed. [Resolve] never changes the state it is
// given; it returns the next one.
//
// # Resolution
//
// [Resolve] applies one round's orders to a [State] under these rules, in
// this order:
//
//  1. Move. Every order applies at once. An order naming an unknown element
//     is ignored, and of several orders for one element the last one wins.
//     A step enters an orthogonally adjacent cell of the same sector, or,
//     from a gate's cell, the cell of the gate it links to. Entering a gate
//     does not traverse it: the traversal is a step of its own. An order
//     with more steps than the element's [Kind.Moves], a step that is
//     neither an adjacency nor a traversal, or a step off the grid or into
//     an obstacle is refused as a whole, and the element stays. An element
//     that would end on the cell where another element of its faction ends
//     is refused too, repeatedly until no two elements of one faction share
//     a cell. Only the final cells matter, so elements that pass through or
//     swap with each other do not meet.
//  2. Engage. In each cell holding both factions' elements, each faction's
//     strength is summed. A tie destroys every element in the cell.
//     Otherwise the weaker faction's elements are destroyed, and the
//     stronger loses the weaker's total, taken from its weakest element
//     first (lowest strength, then lowest ID), an element at zero removed
//     and the remainder carried to the next.
//  3. Capture. An objective whose cell holds elements of exactly one
//     faction becomes that faction's. Any other objective keeps its holder,
//     even when no element stands on it.
//  4. Observe. Each faction observes its own elements, every enemy element
//     and every objective in the same sector within the Chebyshev distance
//     of [Kind.Sight] of any of its elements. Sight ends at the sector's
//     edge, gates included. See [Observe].
//  5. Judge. A faction with no elements loses, and when neither has any the
//     exercise is a draw. Otherwise a faction that holds every objective
//     wins. Otherwise, at the round limit, the faction holding more
//     objectives wins, and equal holdings are a draw. See [Judge].
//
// The package uses the standard library alone.
package rules
