// Package fusion fuses a faction's observations into what intelligence
// knows of an exercise: the faction's own elements, the enemy contacts it
// knows of, and the status of each objective.
//
// The package holds intelligence's own reading of the public ruleset. Its
// [Observation] decodes the exercise.round.observed event, and its
// [Location] the map's objectives the exercise.started event carries,
// because the services share no Go types. It is pure: it performs no I/O,
// and the domain calls it inside a command.
//
// Five suppression rules limit what a [Picture] can hold. exercise's own
// observation enforces two of them, so fusion reads nothing more than the
// observation carries:
//
//   - sight radius by kind: an element sees the Chebyshev distance of its
//     kind's [Sight];
//   - sight stops at the sector's edge: no element sees into another
//     sector, even through a gate.
//
// [Fuse] enforces the other three:
//
//   - a contact is kept at the cell it was last seen in, and dropped once
//     it has gone more than k rounds unseen, or as soon as a round shows
//     that cell to one of the faction's elements without it;
//   - the picture holds nothing a round did not reveal: an objective no
//     element has seen has no known holder, and a contact carries only
//     what its last sighting showed;
//   - the chain's own lag: a picture is of the round its observation
//     reports, and ages count in rounds, so a round the domain never
//     assesses still ages what it knows.
package fusion
