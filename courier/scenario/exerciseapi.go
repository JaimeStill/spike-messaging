package scenario

import (
	"context"
	"errors"
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
	// ruleset is the rules exercise plays by, as far as the scenarios read
	// them: how many rounds in a row a faction must end alone on an
	// objective to take it, and the Chebyshev distance an element of each
	// kind sees within its own sector.
	ruleset struct {
		CaptureRounds int            `json:"capture_rounds"`
		Sight         map[string]int `json:"sight"`
	}
	// exerciseView is what GET /api/exercises/{id} returns, as far as the
	// scenarios read it.
	exerciseView struct {
		Seed    int64         `json:"seed"`
		Rules   ruleset       `json:"rules"`
		State   exerciseState `json:"state"`
		Verdict *verdict      `json:"verdict"`
	}
)

// validate returns an error unless the rules give a positive number of
// capture rounds and a sight for some kind, as exercise's API always does: a
// view without them would narrate or check the exercise wrongly.
func (r ruleset) validate() error {
	var errs []error
	if r.CaptureRounds <= 0 {
		errs = append(errs, errors.New("the rules give no capture_rounds"))
	}
	if len(r.Sight) == 0 {
		errs = append(errs, errors.New("the rules give no sight"))
	}
	return errors.Join(errs...)
}

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
