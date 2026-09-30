package fusion_test

import (
	"cmp"
	"encoding/json"
	"reflect"
	"slices"
	"strconv"
	"testing"

	"github.com/JaimeStill/spike-messaging/services/intelligence/domain/intelligence/fusion"
)

func loc(sector string, x, y int) fusion.Location {
	return fusion.Location{Sector: sector, X: x, Y: y}
}

func squad(id, faction string, strength int, at fusion.Location) fusion.Element {
	return fusion.Element{ID: id, Faction: faction, Kind: "squad", Strength: strength, Health: []int{strength}, Status: "ready", At: at}
}

func scout(id, faction string, at fusion.Location) fusion.Element {
	return fusion.Element{ID: id, Faction: faction, Kind: "scout", Strength: 1, Health: []int{1}, Status: "ready", At: at}
}

// grid is the map the tests open over: sector a, 13 wide and 5 high, and
// sector b, 3 by 3.
var grid = fusion.Map{Sectors: []fusion.Sector{{ID: "a", Width: 13, Height: 5}, {ID: "b", Width: 3, Height: 3}}}

// observe returns red's observation of round: its own elements and the
// contacts it sees.
func observe(round int, own []fusion.Element, contacts ...fusion.Element) fusion.Observation {
	return fusion.Observation{Round: round, Own: own, Contacts: contacts}
}

// ids returns each contact's ID and age as "id@age".
func ids(cs []fusion.Contact) []string {
	out := []string{}
	for _, c := range cs {
		out = append(out, c.ID+"@"+strconv.Itoa(c.Age))
	}
	return out
}

// The map and the observation decode from exercise's events' JSON, a
// location's point flattened beside its sector.
func TestDecodesTheExerciseShape(t *testing.T) {
	var m fusion.Map
	if err := json.Unmarshal([]byte(`{"sectors":[{"id":"a","width":3,"height":1,
		"obstacles":[],"objectives":[],"gates":[]}]}`), &m); err != nil {
		t.Fatal(err)
	}
	if want := []fusion.Sector{{ID: "a", Width: 3, Height: 1}}; !slices.Equal(m.Sectors, want) {
		t.Errorf("Sectors = %v, want %v", m.Sectors, want)
	}
	var o fusion.Observation
	if err := json.Unmarshal([]byte(`{"exercise":"x","faction":"red","round":4,
		"own":[{"id":"r1","faction":"red","kind":"squad","strength":3,"health":[3],"status":"ready","at":{"sector":"a","x":0,"y":0}}],
		"contacts":[{"id":"b1","faction":"blue","kind":"scout","strength":1,"health":[1],"status":"ready","at":{"sector":"a","x":1,"y":0}}],
		"objectives":[{"at":{"sector":"a","x":2,"y":0},"holder":"blue"}]}`), &o); err != nil {
		t.Fatal(err)
	}
	want := fusion.Observation{
		Round:      4,
		Own:        []fusion.Element{squad("r1", "red", 3, loc("a", 0, 0))},
		Contacts:   []fusion.Element{scout("b1", "blue", loc("a", 1, 0))},
		Objectives: []fusion.ObjectiveStatus{{At: loc("a", 2, 0), Holder: "blue"}},
	}
	if !reflect.DeepEqual(o, want) {
		t.Errorf("observation = %+v, want %+v", o, want)
	}
}

// A picture opens with nothing known and nothing explored, and the map's
// grid kept.
func TestOpen(t *testing.T) {
	want := fusion.Picture{
		Round:      -1,
		Own:        []fusion.Element{},
		Contacts:   []fusion.Contact{},
		Objectives: []fusion.Objective{},
		Explored:   []fusion.Location{},
		Grid:       grid.Sectors,
	}
	if p := fusion.Open(grid); !reflect.DeepEqual(p, want) {
		t.Errorf("Open = %+v, want %+v", p, want)
	}
}

// A contact is kept at its last-seen cell while it goes unseen, aging a
// round at a time, and drops once it has gone more than k rounds unseen.
func TestFuseAgesAContactOut(t *testing.T) {
	const k = 2
	far := []fusion.Element{squad("r1", "red", 2, loc("a", 0, 0))}
	p := fusion.Fuse(fusion.Open(grid), observe(0, far, squad("b1", "blue", 3, loc("a", 2, 0))), k)
	for round, want := range [][]string{{"b1@1"}, {"b1@2"}, {}} {
		// r1 moves away along its sector's column, out of sight of 2,0.
		own := []fusion.Element{squad("r1", "red", 2, loc("a", 0, 3+round))}
		p = fusion.Fuse(p, observe(round+1, own), k)
		if got := ids(p.Contacts); !slices.Equal(got, want) {
			t.Errorf("round %d: contacts = %v, want %v", round+1, got, want)
		}
	}
}

// An unseen contact stays where it was last seen, with what that sighting
// showed, however its element has moved since.
func TestFuseKeepsTheLastSighting(t *testing.T) {
	b1 := squad("b1", "blue", 3, loc("a", 2, 0))
	p := fusion.Fuse(fusion.Open(grid), observe(4, []fusion.Element{squad("r1", "red", 2, loc("a", 0, 0))}, b1), 3)
	p = fusion.Fuse(p, observe(5, []fusion.Element{squad("r1", "red", 2, loc("a", 0, 9))}), 3)
	want := []fusion.Contact{{Element: b1, Seen: 4, Age: 1}}
	if !reflect.DeepEqual(p.Contacts, want) {
		t.Errorf("contacts = %+v, want %+v", p.Contacts, want)
	}
}

// A picture shares no health with the observation it was fused from.
func TestFuseCopiesHealth(t *testing.T) {
	own, b1 := squad("r1", "red", 2, loc("a", 0, 0)), squad("b1", "blue", 3, loc("a", 2, 0))
	p := fusion.Fuse(fusion.Open(grid), observe(0, []fusion.Element{own}, b1), 3)
	own.Health[0], b1.Health[0] = 0, 0
	if p.Own[0].Health[0] != 2 || p.Contacts[0].Health[0] != 3 {
		t.Errorf("picture aliases the observation: own %v, contact %v", p.Own[0].Health, p.Contacts[0].Health)
	}
}

// A contact seen again is refreshed where it now stands, at age 0.
func TestFuseRefreshesAContactSeenAgain(t *testing.T) {
	own := []fusion.Element{scout("r1", "red", loc("a", 0, 0))}
	p := fusion.Fuse(fusion.Open(grid), observe(0, own, squad("b1", "blue", 3, loc("a", 4, 0))), 3)
	moved := squad("b1", "blue", 1, loc("a", 3, 3))
	p = fusion.Fuse(p, observe(2, own, moved), 3)
	want := []fusion.Contact{{Element: moved, Seen: 2}}
	if !reflect.DeepEqual(p.Contacts, want) {
		t.Errorf("contacts = %+v, want %+v", p.Contacts, want)
	}
}

// A contact drops as soon as one of the faction's elements sees its
// last-seen cell without it: the round revealed that it is gone. Sight
// follows the kind, and stops at the sector's edge.
func TestFuseDropsAContactRevealedGone(t *testing.T) {
	for name, tc := range map[string]struct {
		own  fusion.Element
		kept bool
	}{
		"a squad one cell away":            {squad("r2", "red", 2, loc("a", 3, 2)), false},
		"a squad two cells away":           {squad("r2", "red", 2, loc("a", 4, 2)), true},
		"a scout two cells away":           {scout("r2", "red", loc("a", 4, 4)), false},
		"a scout three cells away":         {scout("r2", "red", loc("a", 5, 2)), true},
		"standing on the cell":             {squad("r2", "red", 2, loc("a", 2, 2)), false},
		"the same point in another sector": {squad("r2", "red", 2, loc("b", 2, 2)), true},
	} {
		t.Run(name, func(t *testing.T) {
			first := []fusion.Element{scout("r1", "red", loc("a", 0, 0))}
			p := fusion.Fuse(fusion.Open(grid), observe(0, first, squad("b1", "blue", 3, loc("a", 2, 2))), 3)
			p = fusion.Fuse(p, observe(1, []fusion.Element{tc.own}), 3)
			if kept := len(p.Contacts) == 1; kept != tc.kept {
				t.Errorf("kept = %v, want %v", kept, tc.kept)
			}
		})
	}
}

// An objective is listed once an observation reports it, known by the
// holder it had, and ages while unseen, keeping that holder. One seen again
// updates, and the list stays sorted by location.
func TestFuseDiscoversObjectives(t *testing.T) {
	own := []fusion.Element{squad("r1", "red", 2, loc("a", 0, 0))}
	seen := observe(3, own)
	seen.Objectives = []fusion.ObjectiveStatus{{At: loc("b", 1, 1), Holder: ""}, {At: loc("a", 1, 0), Holder: "blue"}}
	p := fusion.Fuse(fusion.Open(grid), seen, 3)
	want := []fusion.Objective{
		{At: loc("a", 1, 0), Holder: "blue", Seen: 3},
		{At: loc("b", 1, 1), Seen: 3},
	}
	if !reflect.DeepEqual(p.Objectives, want) {
		t.Errorf("discovered = %+v, want %+v", p.Objectives, want)
	}
	p = fusion.Fuse(p, observe(5, own), 3)
	want = []fusion.Objective{
		{At: loc("a", 1, 0), Holder: "blue", Seen: 3, Age: 2},
		{At: loc("b", 1, 1), Seen: 3, Age: 2},
	}
	if !reflect.DeepEqual(p.Objectives, want) {
		t.Errorf("aged = %+v, want %+v", p.Objectives, want)
	}
	again := observe(6, own)
	again.Objectives = []fusion.ObjectiveStatus{{At: loc("a", 1, 0), Holder: "red"}, {At: loc("a", 0, 0), Holder: ""}}
	p = fusion.Fuse(p, again, 3)
	want = []fusion.Objective{
		{At: loc("a", 0, 0), Seen: 6},
		{At: loc("a", 1, 0), Holder: "red", Seen: 6},
		{At: loc("b", 1, 1), Seen: 3, Age: 3},
	}
	if !reflect.DeepEqual(p.Objectives, want) {
		t.Errorf("updated = %+v, want %+v", p.Objectives, want)
	}
}

// An alert adds an objective the picture does not list, in order, and
// updates one it does, aged against the picture's round.
func TestAlert(t *testing.T) {
	own := []fusion.Element{squad("r1", "red", 2, loc("a", 0, 0))}
	seen := observe(3, own)
	seen.Objectives = []fusion.ObjectiveStatus{{At: loc("a", 5, 5), Holder: "red"}}
	p := fusion.Fuse(fusion.Open(grid), seen, 3)

	added, changed := fusion.Alert(p, loc("a", 1, 0), "blue", 3)
	if !changed {
		t.Error("an added objective reported no change")
	}
	want := []fusion.Objective{
		{At: loc("a", 1, 0), Holder: "blue", Seen: 3},
		{At: loc("a", 5, 5), Holder: "red", Seen: 3},
	}
	if !reflect.DeepEqual(added.Objectives, want) {
		t.Errorf("added = %+v, want %+v", added.Objectives, want)
	}

	p = fusion.Fuse(p, observe(5, own), 3)
	updated, changed2 := fusion.Alert(p, loc("a", 5, 5), "blue", 4)
	if !changed2 {
		t.Error("an updated objective reported no change")
	}
	want = []fusion.Objective{{At: loc("a", 5, 5), Holder: "blue", Seen: 4, Age: 1}}
	if !reflect.DeepEqual(updated.Objectives, want) {
		t.Errorf("updated = %+v, want %+v", updated.Objectives, want)
	}
	if p.Objectives[0].Holder != "red" {
		t.Error("Alert changed the picture it was given")
	}
}

// An alert that restates the objective the picture lists, as a redelivery
// or a duplicate does, changes nothing.
func TestAlertReportsWhetherItChanged(t *testing.T) {
	p := fusion.Open(grid)
	p.Round = 2
	once, changed := fusion.Alert(p, loc("a", 1, 1), "blue", 2)
	if !changed {
		t.Fatal("the first alert reported no change")
	}
	again, changed := fusion.Alert(once, loc("a", 1, 1), "blue", 2)
	if changed || !reflect.DeepEqual(again, once) {
		t.Errorf("a repeated alert changed = %v, picture %+v, want unchanged", changed, again)
	}
	if _, changed := fusion.Alert(once, loc("a", 1, 1), "red", 2); !changed {
		t.Error("a new holder reported no change")
	}
	if _, changed := fusion.Alert(once, loc("a", 1, 1), "blue", 3); !changed {
		t.Error("a later sighting reported no change")
	}
}

// An alert of a round the picture has not reached is seen this round: age 0.
// A later sighting than the alert's wins.
func TestAlertAgesAndLaterSightingsWin(t *testing.T) {
	p := fusion.Open(grid)
	p.Round = 2
	ahead, _ := fusion.Alert(p, loc("a", 1, 1), "blue", 4)
	if want := (fusion.Objective{At: loc("a", 1, 1), Holder: "blue", Seen: 4}); ahead.Objectives[0] != want {
		t.Errorf("ahead = %+v, want %+v", ahead.Objectives[0], want)
	}
	stale, changed := fusion.Alert(ahead, loc("a", 1, 1), "red", 3)
	if changed {
		t.Error("a stale alert reported a change")
	}
	if !reflect.DeepEqual(stale.Objectives, ahead.Objectives) {
		t.Errorf("stale = %+v, want %+v as it stood", stale.Objectives, ahead.Objectives)
	}
}

// An alert shares nothing mutable with the picture it changes but the grid,
// which nothing changes.
func TestAlertCopies(t *testing.T) {
	own := []fusion.Element{squad("r1", "red", 2, loc("a", 0, 0))}
	p := fusion.Fuse(fusion.Open(grid), observe(1, own, squad("b1", "blue", 3, loc("a", 2, 0))), 3)
	q, _ := fusion.Alert(p, loc("a", 1, 1), "blue", 1)
	q.Own[0].Health[0] = 99
	q.Contacts[0].Health[0] = 99
	q.Objectives[0].Holder = "x"
	if p.Own[0].Health[0] != 2 || p.Contacts[0].Health[0] != 3 || len(p.Objectives) != 0 {
		t.Errorf("picture changed through the alert's copy: %+v", p)
	}
}

// An objective an alert recorded from a round the observation predates
// stands, and ages no less than 0; the round's own observation replaces it.
func TestFuseKeepsAnAlertedObjective(t *testing.T) {
	own := []fusion.Element{squad("r1", "red", 2, loc("a", 0, 0))}
	p := fusion.Fuse(fusion.Open(grid), observe(3, own), 3)
	p, _ = fusion.Alert(p, loc("a", 1, 0), "blue", 5)
	late := observe(4, own)
	late.Objectives = []fusion.ObjectiveStatus{{At: loc("a", 1, 0), Holder: "red"}}
	want := []fusion.Objective{{At: loc("a", 1, 0), Holder: "blue", Seen: 5}}
	if got := fusion.Fuse(p, late, 3).Objectives; !reflect.DeepEqual(got, want) {
		t.Errorf("fused over = %+v, want %+v", got, want)
	}
	if got := fusion.Fuse(p, observe(5, own), 3).Objectives; !reflect.DeepEqual(got, want) {
		t.Errorf("fused at = %+v, want %+v", got, want)
	}
}

// The explored cells grow with what the own elements see: a squad one cell
// around it, a scout two, never past the edge of its sector's grid. They
// accumulate over rounds, hold each cell once, and stay sorted by sector,
// y, then x.
func TestFuseExplores(t *testing.T) {
	p := fusion.Fuse(fusion.Open(grid), observe(0, []fusion.Element{squad("r1", "red", 2, loc("a", 0, 0))}), 3)
	want := []fusion.Location{loc("a", 0, 0), loc("a", 1, 0), loc("a", 0, 1), loc("a", 1, 1)}
	if !slices.Equal(p.Explored, want) {
		t.Errorf("squad at the corner explored %v, want %v", p.Explored, want)
	}

	p = fusion.Fuse(p, observe(1, []fusion.Element{scout("r2", "red", loc("a", 12, 4))}), 3)
	if len(p.Explored) != 4+9 {
		t.Errorf("after the scout at the far corner: %d cells %v, want 13", len(p.Explored), p.Explored)
	}
	for _, at := range p.Explored {
		if at.X < 0 || at.X > 12 || at.Y < 0 || at.Y > 4 {
			t.Errorf("explored %v outside the grid", at)
		}
	}
	if !slices.Contains(p.Explored, loc("a", 10, 2)) || slices.Contains(p.Explored, loc("a", 9, 2)) {
		t.Errorf("the scout's sight is two cells: %v", p.Explored)
	}

	own := []fusion.Element{squad("r1", "red", 2, loc("a", 1, 1)), scout("r2", "red", loc("b", 1, 1))}
	p = fusion.Fuse(p, observe(2, own), 3)
	if len(p.Explored) != 13+5+9 {
		t.Errorf("after a squad overlapping the first and a scout in b: %d cells, want %d", len(p.Explored), 27)
	}
	sorted := slices.IsSortedFunc(p.Explored, func(a, b fusion.Location) int {
		return cmp.Or(cmp.Compare(a.Sector, b.Sector), cmp.Compare(a.Y, b.Y), cmp.Compare(a.X, b.X))
	})
	if !sorted {
		t.Errorf("explored is not sorted: %v", p.Explored)
	}
	if len(slices.Compact(slices.Clone(p.Explored))) != len(p.Explored) {
		t.Errorf("explored holds a cell twice: %v", p.Explored)
	}

	if p = fusion.Fuse(p, observe(3, nil), 3); len(p.Explored) != 27 {
		t.Errorf("a round with no element lost explored cells: %d", len(p.Explored))
	}
}

// Ages count in rounds, so a round never assessed still ages a contact:
// a skipped round costs the picture nothing but its lag.
func TestFuseAgesAcrossASkippedRound(t *testing.T) {
	own := []fusion.Element{squad("r1", "red", 2, loc("a", 0, 0))}
	p := fusion.Fuse(fusion.Open(grid), observe(2, own, squad("b1", "blue", 3, loc("b", 0, 0))), 2)
	p = fusion.Fuse(p, observe(4, own), 2)
	if got := ids(p.Contacts); !slices.Equal(got, []string{"b1@2"}) {
		t.Errorf("round 4: contacts = %v", got)
	}
	p = fusion.Fuse(p, observe(7, own), 2)
	if len(p.Contacts) != 0 {
		t.Errorf("round 7: contacts = %v, want none", ids(p.Contacts))
	}
}

// The picture takes the observed own elements as they are, sorted, and
// sorts its contacts by ID.
func TestFuseSorts(t *testing.T) {
	own := []fusion.Element{squad("r2", "red", 2, loc("a", 0, 0)), squad("r1", "red", 2, loc("a", 1, 0))}
	p := fusion.Fuse(fusion.Open(grid), observe(0, own,
		squad("b2", "blue", 1, loc("a", 2, 0)), squad("b1", "blue", 1, loc("a", 3, 0))), 3)
	if p.Own[0].ID != "r1" || p.Contacts[0].ID != "b1" {
		t.Errorf("own = %v, contacts = %v", p.Own, ids(p.Contacts))
	}
	if p.Round != 0 {
		t.Errorf("round = %d", p.Round)
	}
}
