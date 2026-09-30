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
//     swap with each other do not meet. An element that starts the round in
//     a cell holding the other faction's elements is pinned in the fight
//     there: its order is refused, and it stays.
//  2. Engage. In each cell holding both factions' elements, each faction's
//     strength is summed, and each faction loses half the other's total,
//     rounded up, both at once: a fight is attrition, and lasts as many
//     rounds as both factions stay in the cell. A faction's loss is taken
//     from its weakest element first (lowest strength, then lowest ID); an
//     element reduced to zero is removed, and the rest of the loss carries
//     to the next element.
//  3. Capture. An objective whose cell holds elements of exactly one
//     faction becomes that faction's. Any other objective keeps its holder,
//     even when no element stands on it.
//  4. Observe. Each faction observes its own elements, and every enemy
//     element and objective that lies in the same sector as one of its
//     elements and within that element's [Kind.Sight], a Chebyshev
//     distance. Sight ends at the sector's
//     edge, gates included. See [Observe].
//  5. Judge. A faction with no elements loses, and when neither has any the
//     exercise is a draw. Otherwise a faction that holds every objective
//     wins. Otherwise, at the round limit, the faction holding more
//     objectives wins, and equal holdings are a draw. See [Judge].
//
// The package uses the standard library alone.
package rules
