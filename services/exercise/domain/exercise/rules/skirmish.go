package rules

import (
	"cmp"
	"fmt"
	"math/rand/v2"
	"slices"
)

// The skirmish's layout.
const (
	// SkirmishSize is the width and height of the skirmish's one sector.
	SkirmishSize = 13
	// SkirmishSector is the ID of the skirmish's one sector.
	SkirmishSector = "field"
)

// Skirmish returns the start of a skirmish between factions, drawn from
// seed alone, so one seed always lays out the same skirmish:
//
//   - One [SkirmishSize] square sector with no obstacles and no gates.
//   - Three to five objectives on rows 3 to 9.
//   - Each faction's three squads of four operators and two scouts, every
//     operator at full health, on distinct cells of its home baseline:
//     row 0 for factions[0], and the last row for factions[1].
//
// Every objective and element is mirrored through the center, so neither
// faction is favored; an odd number of objectives puts one at the center.
// Element IDs are each faction's initial and a number, squads first, or
// the faction's whole name when both factions share an initial.
func Skirmish(seed int64, factions [2]string) State {
	rng := rand.New(rand.NewPCG(uint64(seed), 0))
	last := SkirmishSize - 1
	mirror := func(p Point) Point { return Point{X: last - p.X, Y: last - p.Y} }
	center := Point{X: last / 2, Y: last / 2}

	n := 3 + rng.IntN(3)
	var objectives []Point
	if n%2 == 1 {
		objectives = append(objectives, center)
	}
	// The north half of rows 3 to 9: the rows above the center, and the
	// cells left of it on its row. Each cell's mirror lies in the south half.
	var half []Point
	for y := 3; y <= last/2; y++ {
		for x := range SkirmishSize {
			if p := (Point{X: x, Y: y}); y < center.Y || x < center.X {
				half = append(half, p)
			}
		}
	}
	for _, i := range rng.Perm(len(half))[:n/2] {
		objectives = append(objectives, half[i], mirror(half[i]))
	}
	slices.SortFunc(objectives, func(a, b Point) int {
		return cmp.Or(cmp.Compare(a.Y, b.Y), cmp.Compare(a.X, b.X))
	})

	prefix := [2]string{factions[0][:min(1, len(factions[0]))], factions[1][:min(1, len(factions[1]))]}
	if prefix[0] == prefix[1] {
		prefix = factions
	}
	kinds := []Kind{Squad, Squad, Squad, Scout, Scout}
	var elements []Element
	for i, x := range rng.Perm(SkirmishSize)[:len(kinds)] {
		home := Point{X: x, Y: 0}
		for side, at := range []Point{home, mirror(home)} {
			k := kinds[i]
			health := slices.Repeat([]int{100}, k.Operators())
			elements = append(elements, Element{
				ID:       fmt.Sprintf("%s%d", prefix[side], i+1),
				Faction:  factions[side],
				Kind:     k,
				Strength: sum(health),
				Health:   health,
				Status:   StatusReady,
				At:       Location{Sector: SkirmishSector, Point: at},
			})
		}
	}
	slices.SortFunc(elements, func(a, b Element) int { return cmp.Compare(a.ID, b.ID) })
	return State{
		Map: Map{Sectors: []Sector{{
			ID: SkirmishSector, Width: SkirmishSize, Height: SkirmishSize,
			Obstacles: []Point{}, Objectives: objectives, Gates: []Gate{},
		}}},
		Factions: factions,
		Elements: elements,
		Holders:  map[string]string{},
		Progress: map[string]Progress{},
	}
}
