package scenario

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// The observer's view of an exercise, as exercise's API gives it: the
// theater and the theater check each read what the factions are not told.
type (
	// exerciseState is an exercise's state: its map with every objective,
	// its factions, and who holds each objective, by place.
	exerciseState struct {
		Map struct {
			Sectors []struct {
				ID         string  `json:"id"`
				Objectives []point `json:"objectives"`
			} `json:"sectors"`
		} `json:"map"`
		Factions []string          `json:"factions"`
		Holders  map[string]string `json:"holders"`
	}
	// exerciseView is what GET /api/exercises/{id} returns, as far as the
	// scenarios read it.
	exerciseView struct {
		Seed    int64            `json:"seed"`
		State   exerciseState    `json:"state"`
		Verdict *observerVerdict `json:"verdict"`
	}
)

// objectives returns every objective's cell, in the map's order.
func (s exerciseState) objectives() []location {
	var out []location
	for _, sec := range s.Map.Sectors {
		for _, p := range sec.Objectives {
			out = append(out, location{Sector: sec.ID, point: p})
		}
	}
	return out
}

// exerciseEndpoint returns the URL of an exercise's API under base.
func exerciseEndpoint(base, exercise string) string {
	return strings.TrimSuffix(base, "/") + "/api/exercises/" + exercise
}

// readExercise reads an exercise's view from exercise's API.
func readExercise(ctx context.Context, base, exercise string) (exerciseView, error) {
	var v exerciseView
	err := getJSON(ctx, exerciseEndpoint(base, exercise), &v)
	return v, err
}

// validExerciseURL returns an error unless u is an absolute URL.
func validExerciseURL(u string) error {
	if p, err := url.Parse(u); err != nil || p.Scheme == "" || p.Host == "" {
		return fmt.Errorf("--exercise-url must be an absolute URL")
	}
	return nil
}
