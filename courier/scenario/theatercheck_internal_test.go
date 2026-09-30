package scenario

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// decode reads v's JSON form into a T, as the check reads exercise's API
// and the stream.
func decode[T any](t *testing.T, v any) T {
	t.Helper()
	body, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out T
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func objective(x, y int, holder string, seen, age int) map[string]any {
	return map[string]any{"at": cell("a", x, y), "holder": holder, "known": true, "seen": seen, "age": age}
}

func contact(e map[string]any, seen, age int) map[string]any {
	c := map[string]any{"seen": seen, "age": age}
	for k, v := range e {
		c[k] = v
	}
	return c
}

// umpireOf is a round of history: the state's holders and resolution's
// captures, and each faction's observation, red's then blue's.
func umpireOf(round int, holders map[string]string, captures []any, red, blue map[string]any) map[string]any {
	h := map[string]any{
		"round": round,
		"state": map[string]any{
			"map": map[string]any{"sectors": []any{map[string]any{"id": "a", "objectives": []any{
				map[string]any{"x": 1, "y": 1}, map[string]any{"x": 4, "y": 4}}}}},
			"factions": []string{"red", "blue"},
			"holders":  holders,
		},
		"observations": []any{red, blue},
	}
	if captures != nil {
		h["resolution"] = map[string]any{"captures": captures}
	}
	return h
}

func seeing(own, contacts, objectives []any) map[string]any {
	return map[string]any{"own": own, "contacts": contacts, "objectives": objectives}
}

func assessed(round int, faction string, own, contacts, objectives []any) map[string]any {
	return map[string]any{"exercise": theaterID, "faction": faction, "round": round,
		"own": own, "contacts": contacts, "objectives": objectives}
}

// fixture is a three-round exercise on sector a, with objectives at 1,1
// and 4,4, and its assessments, each consistent with it. Red's squad r1
// takes 1,1 in round 1 and leaves it; blue takes it in round 2, and a loss
// alert tells red, which no longer sees it. Blue's scout b1 sees r1 in
// round 0, then remembers it. intelligence revises red's round-2
// assessment for the alert.
func fixture(t *testing.T) ([]umpireRound, []map[string]any) {
	r1 := func(x, y int) map[string]any { return sq("r1", "squad", cell("a", x, y), 100) }
	b1 := func(x, y int) map[string]any { return sq("b1", "scout", cell("a", x, y), 80) }
	seen := func(x, y int, holder string) map[string]any {
		return map[string]any{"at": cell("a", x, y), "holder": holder}
	}
	history := decode[[]umpireRound](t, []any{
		umpireOf(0, map[string]string{}, nil,
			seeing([]any{r1(0, 0)}, []any{}, []any{seen(1, 1, "")}),
			seeing([]any{b1(2, 0)}, []any{r1(0, 0)}, []any{seen(1, 1, "")})),
		umpireOf(1, map[string]string{"a:1,1": "red"},
			[]any{map[string]any{"at": cell("a", 1, 1), "faction": "red", "from": ""}},
			seeing([]any{r1(1, 1)}, []any{}, []any{seen(1, 1, "red")}),
			seeing([]any{b1(4, 2)}, []any{}, []any{seen(4, 4, "")})),
		umpireOf(2, map[string]string{"a:1,1": "blue"},
			[]any{map[string]any{"at": cell("a", 1, 1), "faction": "blue", "from": "red"}},
			seeing([]any{r1(0, 4)}, []any{}, []any{}),
			seeing([]any{b1(4, 2)}, []any{}, []any{seen(4, 4, "")})),
	})
	return history, []map[string]any{
		assessed(0, "red", []any{r1(0, 0)}, []any{}, []any{objective(1, 1, "", 0, 0)}),
		assessed(0, "blue", []any{b1(2, 0)}, []any{contact(r1(0, 0), 0, 0)}, []any{objective(1, 1, "", 0, 0)}),
		assessed(1, "red", []any{r1(1, 1)}, []any{}, []any{objective(1, 1, "red", 1, 0)}),
		assessed(1, "blue", []any{b1(4, 2)}, []any{contact(r1(0, 0), 0, 1)},
			[]any{objective(1, 1, "", 0, 1), objective(4, 4, "", 1, 0)}),
		assessed(2, "red", []any{r1(0, 4)}, []any{}, []any{objective(1, 1, "red", 1, 1)}),
		assessed(2, "blue", []any{b1(4, 2)}, []any{contact(r1(0, 0), 0, 2)},
			[]any{objective(1, 1, "", 0, 2), objective(4, 4, "", 2, 0)}),
		// The revision: 1,1 is out of red's sight, as the alert has it.
		assessed(2, "red", []any{r1(0, 4)}, []any{}, []any{objective(1, 1, "blue", 2, 0)}),
	}
}

func check(t *testing.T, history []umpireRound, raw []map[string]any, k int) ([]string, int) {
	t.Helper()
	all := make([]checkedAssessment, len(raw))
	for i, a := range raw {
		all[i] = decode[checkedAssessment](t, a)
	}
	return checkTheater(theaterID, history, &umpireVerdict{Winner: "blue", Reason: "limit"}, all, k)
}

// Assessments that follow the suppression rules are consistent, the last
// of a round's revisions standing, and an objective a loss alert told of
// is accepted out of sight. The report lists what each faction believes of
// every objective, and the verdict.
func TestTheaterCheckAcceptsConsistentAssessments(t *testing.T) {
	history, raw := fixture(t)
	lines, errs := check(t, history, raw, 3)
	want := []string{
		"theater  " + theaterID,
		"  rounds          3",
		"  assessments     7, 1 revised",
		"  contact rounds  3",
		"consistent  every assessment matches the umpire's record under the suppression rules",
		"beliefs  red",
		"  objective:1,1  blue          truly blue",
		"  objective:4,4  undiscovered  truly unheld",
		"beliefs  blue",
		"  objective:1,1  unheld (seen 2 ago)  truly blue",
		"  objective:4,4  unheld               truly unheld",
		"verdict  blue by limit",
		"truly held",
		"  blue  objective:1,1",
	}
	if errs != 0 || !slices.Equal(lines, want) {
		t.Errorf("%d inconsistencies; report:\n%s\n\nwant:\n%s", errs, strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
}

// Each departure from the suppression rules is an inconsistency.
func TestTheaterCheckFindsInconsistencies(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(as []map[string]any) []map[string]any
		want   string
	}{
		{"a missing assessment", func(as []map[string]any) []map[string]any { return slices.Delete(as, 3, 4) },
			"round 1 blue: no assessment was issued"},
		{"an assessment of no round", func(as []map[string]any) []map[string]any {
			return append(as, assessed(3, "red", []any{}, []any{}, []any{}))
		}, "round 3 red: assessed, but exercise's history has no such round or faction"},
		{"own elements", func(as []map[string]any) []map[string]any {
			as[2]["own"] = []any{sq("r1", "squad", cell("a", 1, 1), 90)}
			return as
		}, "round 1 red: own elements differ from the truth"},
		{"an enemy in sight unreported", func(as []map[string]any) []map[string]any {
			as[1]["contacts"] = []any{}
			return as
		}, "round 0 blue: r1 is in sight at a:0,0, strength 100, but not reported so"},
		{"an enemy reported in sight that is not", func(as []map[string]any) []map[string]any {
			as[3]["contacts"] = []any{contact(sq("r1", "squad", cell("a", 0, 0), 100), 1, 0)}
			return as
		}, "round 1 blue: r1 is reported in sight, but is not"},
		{"a contact remembered in a cell in sight", func(as []map[string]any) []map[string]any {
			as[2]["contacts"] = []any{contact(sq("b1", "scout", cell("a", 1, 0), 80), 0, 1)}
			return as
		}, "round 1 red: b1 is remembered at a:1,0, a cell in sight"},
		{"a contact remembered too long", func(as []map[string]any) []map[string]any {
			as[5]["contacts"] = []any{contact(sq("r1", "squad", cell("a", 0, 0), 100), 0, 4)}
			return as
		}, "round 2 blue: r1 is remembered 4 rounds, past 3"},
		{"an objective in sight unreported", func(as []map[string]any) []map[string]any {
			as[3]["objectives"] = []any{objective(1, 1, "", 0, 1)}
			return as
		}, "round 1 blue: objective:4,4 is in sight, but not reported"},
		{"an objective in sight with the wrong holder", func(as []map[string]any) []map[string]any {
			as[2]["objectives"] = []any{objective(1, 1, "", 1, 0)}
			return as
		}, "round 1 red: objective:1,1 is in sight and held by red, but not reported so"},
		{"an objective never in sight", func(as []map[string]any) []map[string]any {
			as[0]["objectives"] = []any{objective(1, 1, "", 0, 0), objective(4, 4, "", 0, 0)}
			return as
		}, "round 0 red: objective:4,4 is reported, but red has never had it in sight"},
		{"an objective out of sight seen this round", func(as []map[string]any) []map[string]any {
			as[5]["objectives"] = []any{objective(1, 1, "", 2, 0), objective(4, 4, "", 2, 0)}
			return as
		}, "round 2 blue: objective:1,1 is reported seen this round, but is out of sight"},
		{"an objective out of sight no alert told of", func(as []map[string]any) []map[string]any {
			as[6]["objectives"] = []any{objective(1, 1, "red", 2, 0)}
			return as
		}, "round 2 red: objective:1,1 is reported seen this round, but is out of sight"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			history, raw := fixture(t)
			lines, errs := check(t, history, tc.change(raw), 3)
			report := strings.Join(lines, "\n")
			if errs != 1 || !strings.Contains(report, "inconsistencies  1\n  "+tc.want+"\n") {
				t.Errorf("%d inconsistencies, want 1: %q; report:\n%s", errs, tc.want, report)
			}
		})
	}
}
