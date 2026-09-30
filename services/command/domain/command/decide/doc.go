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
// [Decide] applies its rules to each element, in ID order, and the first
// that applies decides:
//
//  1. retreat: an engaged element steps out of its fight when it is a
//     scout, or when its faction's strength in its cell, summed over all
//     its elements there, is below two thirds of the enemy's, summed over
//     the contacts seen there this round. It steps to the orthogonally
//     adjacent open cell holding no contact seen this round that lies
//     farthest, by Chebyshev distance, from the nearest known contact
//     outside the fight, ties going to the first of up, right, down, left.
//     Its contact is the strongest enemy in the fight;
//  2. pursue or engage in place: an engaged element that does not retreat,
//     or has no cell to retreat to, holds its fight, its target its own
//     cell and its contact the strongest enemy there. Its rule is pursue
//     when its faction's strength in the cell is at least the enemy's, and
//     engage otherwise;
//  3. reinforce: a ready or recovering squad heads for the nearest cell
//     within [ReinforceRange] steps where one of its faction's elements
//     holds its fight, by pursue or engage, its contact the strongest
//     enemy there. Any number of squads may reinforce one fight;
//  4. engage: a squad heads for the nearest known contact weaker than
//     itself within [EngageRange] steps, at the cell it was last seen in;
//  5. secure: otherwise an element heads for the nearest known objective
//     its faction does not hold that none of its other elements is heading
//     for. A scout secures only when no unexplored cell is in its reach;
//  6. search: otherwise an element heads for the nearest unexplored cell
//     that none of its other elements is searching, preferring one farther
//     than [SpreadRange] from every cell they search, so the searchers
//     spread. Scouts pick before squads;
//  7. rescout: otherwise an element heads for the discovered objective its
//     faction does not hold that was seen longest ago, ties going to the
//     nearest, that none of its other elements is rescouting and that it
//     does not stand on. It may head for one another element secures, so
//     elements left idle refresh the faction's beliefs;
//  8. hold: otherwise it stands where it is.
//
// An element is engaged when the assessment gives it that status and a
// contact was seen in its cell this round. A retreat draws fire only from
// the enemy elements that pursue it, and costs the element the next round,
// so a squad leaves only a losing fight; a scout, which never seeks a fight,
// leaves any. An element that at least matches the enemy in its cell
// pursues, so an enemy that retreats from it does not escape unhurt.
//
// Objectives are hidden: the assessment lists only those the faction has
// discovered, and the cells its elements have ever had in sight. A cell no
// element has had in sight is unexplored, and searching the map is how a
// faction discovers the objectives it secures.
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
// An element keeps the objective it was securing or rescouting while that
// objective is still one to secure and it can still reach it, and the cell
// it was searching while that cell is still unexplored, so a faction's
// targets do not trade places as its elements move. Those standing targets
// are claimed before any element picks a new one under the same rule:
// standing objectives before any objective, within each kind standing
// search cells before any search cell, and standing rescouts before any
// rescout.
package decide
