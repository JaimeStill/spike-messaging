package fusion_test

import (
	"encoding/json"
	"reflect"
	"slices"
	"strconv"
	"testing"

	"github.com/JaimeStill/spike-messaging/services/intelligence/domain/intelligence/fusion"
)

func loc(sector string, x, y int) fusion.Location {
	return fusion.Location{Sector: sector, Point: fusion.Point{X: x, Y: y}}
}

func force(id, faction string, strength int, at fusion.Location) fusion.Element {
	return fusion.Element{ID: id, Faction: faction, Kind: "force", Strength: strength, At: at}
}

func scout(id, faction string, at fusion.Location) fusion.Element {
	return fusion.Element{ID: id, Faction: faction, Kind: "scout", Strength: 1, At: at}
}

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
		"objectives":[{"x":2,"y":0}],"gates":[]}]}`), &m); err != nil {
		t.Fatal(err)
	}
	if got := m.Objectives(); !slices.Equal(got, []fusion.Location{loc("a", 2, 0)}) {
		t.Errorf("Objectives = %v", got)
	}
	var o fusion.Observation
	if err := json.Unmarshal([]byte(`{"exercise":"x","faction":"red","round":4,
		"own":[{"id":"r1","faction":"red","kind":"force","strength":3,"at":{"sector":"a","x":0,"y":0}}],
		"contacts":[{"id":"b1","faction":"blue","kind":"scout","strength":1,"at":{"sector":"a","x":1,"y":0}}],
		"objectives":[{"at":{"sector":"a","x":2,"y":0},"holder":"blue"}]}`), &o); err != nil {
		t.Fatal(err)
	}
	want := fusion.Observation{
		Round:      4,
		Own:        []fusion.Element{force("r1", "red", 3, loc("a", 0, 0))},
		Contacts:   []fusion.Element{scout("b1", "blue", loc("a", 1, 0))},
		Objectives: []fusion.ObjectiveStatus{{At: loc("a", 2, 0), Holder: "blue"}},
	}
	if !reflect.DeepEqual(o, want) {
		t.Errorf("observation = %+v, want %+v", o, want)
	}
}

func TestOpen(t *testing.T) {
	p := fusion.Open([]fusion.Location{loc("b", 0, 0), loc("a", 2, 0)})
	want := fusion.Picture{
		Round:    -1,
		Own:      []fusion.Element{},
		Contacts: []fusion.Contact{},
		Objectives: []fusion.Objective{
			{At: loc("a", 2, 0), Seen: -1},
			{At: loc("b", 0, 0), Seen: -1},
		},
	}
	if !reflect.DeepEqual(p, want) {
		t.Errorf("Open = %+v, want %+v", p, want)
	}
}

// A contact is kept at its last-seen cell while it goes unseen, aging a
// round at a time, and drops once it has gone more than k rounds unseen.
func TestFuseAgesAContactOut(t *testing.T) {
	const k = 2
	far := []fusion.Element{force("r1", "red", 2, loc("a", 0, 0))}
	p := fusion.Fuse(fusion.Open(nil), observe(0, far, force("b1", "blue", 3, loc("a", 2, 0))), k)
	for round, want := range [][]string{{"b1@1"}, {"b1@2"}, {}} {
		// r1 moves away along its sector's column, out of sight of 2,0.
		own := []fusion.Element{force("r1", "red", 2, loc("a", 0, 3+round))}
		p = fusion.Fuse(p, observe(round+1, own), k)
		if got := ids(p.Contacts); !slices.Equal(got, want) {
			t.Errorf("round %d: contacts = %v, want %v", round+1, got, want)
		}
	}
}

// An unseen contact stays where it was last seen, with what that sighting
// showed, however its element has moved since.
func TestFuseKeepsTheLastSighting(t *testing.T) {
	b1 := force("b1", "blue", 3, loc("a", 2, 0))
	p := fusion.Fuse(fusion.Open(nil), observe(4, []fusion.Element{force("r1", "red", 2, loc("a", 0, 0))}, b1), 3)
	p = fusion.Fuse(p, observe(5, []fusion.Element{force("r1", "red", 2, loc("a", 0, 9))}), 3)
	want := []fusion.Contact{{Element: b1, Seen: 4, Age: 1}}
	if !reflect.DeepEqual(p.Contacts, want) {
		t.Errorf("contacts = %+v, want %+v", p.Contacts, want)
	}
}

// A contact seen again is refreshed where it now stands, at age 0.
func TestFuseRefreshesAContactSeenAgain(t *testing.T) {
	own := []fusion.Element{scout("r1", "red", loc("a", 0, 0))}
	p := fusion.Fuse(fusion.Open(nil), observe(0, own, force("b1", "blue", 3, loc("a", 4, 0))), 3)
	moved := force("b1", "blue", 1, loc("a", 3, 3))
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
		"a force two cells away":           {force("r2", "red", 2, loc("a", 4, 2)), false},
		"a force three cells away":         {force("r2", "red", 2, loc("a", 5, 2)), true},
		"a scout four cells away":          {scout("r2", "red", loc("a", 6, 6)), false},
		"a scout five cells away":          {scout("r2", "red", loc("a", 7, 2)), true},
		"standing on the cell":             {force("r2", "red", 2, loc("a", 2, 2)), false},
		"the same point in another sector": {force("r2", "red", 2, loc("b", 2, 2)), true},
	} {
		t.Run(name, func(t *testing.T) {
			first := []fusion.Element{scout("r1", "red", loc("a", 0, 0))}
			p := fusion.Fuse(fusion.Open(nil), observe(0, first, force("b1", "blue", 3, loc("a", 2, 2))), 3)
			p = fusion.Fuse(p, observe(1, []fusion.Element{tc.own}), 3)
			if kept := len(p.Contacts) == 1; kept != tc.kept {
				t.Errorf("kept = %v, want %v", kept, tc.kept)
			}
		})
	}
}

// An objective no element has seen stays unknown. One seen is known by
// the holder it had, and ages while unseen, keeping that holder.
func TestFuseObjectives(t *testing.T) {
	p := fusion.Open([]fusion.Location{loc("a", 2, 0), loc("b", 1, 1)})
	own := []fusion.Element{force("r1", "red", 2, loc("a", 0, 0))}
	seen := observe(3, own)
	seen.Objectives = []fusion.ObjectiveStatus{{At: loc("a", 2, 0), Holder: "blue"}}
	p = fusion.Fuse(p, seen, 3)
	p = fusion.Fuse(p, observe(5, own), 3)
	want := []fusion.Objective{
		{At: loc("a", 2, 0), Holder: "blue", Known: true, Seen: 3, Age: 2},
		{At: loc("b", 1, 1), Seen: -1},
	}
	if !reflect.DeepEqual(p.Objectives, want) {
		t.Errorf("objectives = %+v, want %+v", p.Objectives, want)
	}
}

// Ages count in rounds, so a round never assessed still ages a contact:
// a skipped round costs the picture nothing but its lag.
func TestFuseAgesAcrossASkippedRound(t *testing.T) {
	own := []fusion.Element{force("r1", "red", 2, loc("a", 0, 0))}
	p := fusion.Fuse(fusion.Open(nil), observe(2, own, force("b1", "blue", 3, loc("b", 0, 0))), 2)
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
	own := []fusion.Element{force("r2", "red", 2, loc("a", 0, 0)), force("r1", "red", 2, loc("a", 1, 0))}
	p := fusion.Fuse(fusion.Open(nil), observe(0, own,
		force("b2", "blue", 1, loc("a", 2, 0)), force("b1", "blue", 1, loc("a", 3, 0))), 3)
	if p.Own[0].ID != "r1" || p.Contacts[0].ID != "b1" {
		t.Errorf("own = %v, contacts = %v", p.Own, ids(p.Contacts))
	}
	if p.Round != 0 {
		t.Errorf("round = %d", p.Round)
	}
}
