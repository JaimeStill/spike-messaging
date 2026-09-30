// Package rules is the world model of an exercise of sector dominance, and
// the resolution of its rounds. Two factions move elements across the
// sectors of a public [Map], contest its objectives, and see only what their
// elements can see. [Skirmish] lays out the demonstration's world from a
// seed.
//
// The package is pure: its functions take and return values, and do no I/O
// and read no clock. The randomness a round draws comes from the exercise's
// seed and the round alone, in a fixed order, so a round resolves the same
// way every time it is resolved, and a seed replays an exercise. [Resolve]
// never changes the state it is given; it returns the next one.
//
// # Elements
//
// A squad fields up to four operators, makes one move per round, and sees
// one cell. A scout is one operator, makes two moves, and sees two. Each
// operator has health from 1 to 100, and an element's strength is its
// operators' total. An element is ready, engaged in a fight, or recovering
// from a retreat.
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
//     an obstacle is refused as a whole, and the element stays. Only the
//     final cells matter, so elements that pass through or swap with each
//     other do not meet.
//     - An element that would end on the cell where another element of its
//     faction ends is refused, repeatedly until no two elements of one
//     faction share a cell, except in a cell that held a fight at the
//     round's start: reinforcements may join their own elements there.
//     - An element that starts the round in a fight is pinned there: its
//     order is refused, unless it is a retreat of one step.
//     - A recovering element's order is refused.
//  2. Volley. The enemy that stays in the cell a retreat left fires once at
//     the retreating element, which does not fire back.
//  3. Fight. In each cell holding both factions' elements, every living
//     operator fires once at a random living enemy operator in the cell,
//     all at once. A shot hits at [HitChance] and takes [MinDamage] to
//     [MaxDamage] health. An operator at 0 falls, and an element with no
//     operators is destroyed. A fight lasts as long as both factions stay
//     in the cell.
//  4. Status. An element that retreated is recovering for the next round;
//     one in a cell with the enemy is engaged; any other is ready.
//  5. Capture. An objective changes hands once one faction ends
//     [CaptureRounds] rounds in a row alone on it. A round that ends with
//     both factions on it, or neither, resets the count. An objective keeps
//     its holder while no one takes it, even when no element stands on it.
//  6. Observe. Each faction observes its own elements, and every enemy
//     element and objective that lies in the same sector as one of its
//     elements and within that element's [Kind.Sight], a Chebyshev
//     distance. Sight ends at the sector's edge, gates included. See
//     [Observe].
//  7. Judge. A faction with no elements loses, and when neither has any the
//     exercise is a draw. Otherwise a faction that holds every objective
//     wins. Otherwise, at the round limit, the faction holding more
//     objectives wins, and equal holdings are a draw. See [Judge].
//
// The package uses the standard library alone.
package rules
