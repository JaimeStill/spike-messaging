package scenario_test

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/JaimeStill/spike-messaging/courier/output"
	"github.com/JaimeStill/spike-messaging/courier/scenario"
)

func reporter() (*scenario.Reporter, *bytes.Buffer) {
	var out bytes.Buffer
	return scenario.NewReporter(output.New(&out, &bytes.Buffer{})), &out
}

var update = flag.Bool("update", false, "rewrite the golden files under testdata")

// eventID matches a published event's ID in a narration.
var eventID = regexp.MustCompile(`published event \S+`)

// golden compares got's narrated lines with testdata/name.golden, or
// rewrites the file under -update. A golden file pins every line a scenario
// narrates, in order, so a refactor that changes one of them fails. The
// narration runs beside the steps, so the step headings, the blank lines
// between them, and "ready" are dropped, and an event ID is masked.
func golden(t *testing.T, name, got string) {
	t.Helper()
	var lines []string
	for l := range strings.Lines(got) {
		if l == "\n" || strings.HasPrefix(l, "[") || l == "  ready\n" {
			continue
		}
		lines = append(lines, eventID.ReplaceAllString(l, "published event ID"))
	}
	got = strings.Join(lines, "")
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("narration differs from %s:\n--- got\n%s\n--- want\n%s", path, got, want)
	}
}

func TestRunNarratesStepsAndCleansUp(t *testing.T) {
	var ran []string
	cleaned := false
	s := scenario.Scenario{
		Name: "demo",
		Steps: func() ([]scenario.Step, func() error) {
			step := func(name string, err error) scenario.Step {
				return scenario.Step{Intent: name, Action: func(context.Context, *scenario.Reporter) error {
					ran = append(ran, name)
					return err
				}}
			}
			return []scenario.Step{step("one", nil), step("two", errors.New("boom")), step("three", nil)},
				func() error { cleaned = true; return nil }
		},
	}
	rep, out := reporter()
	err := scenario.Run(t.Context(), s, rep)
	if err == nil || !strings.Contains(err.Error(), "demo: step 2: boom") {
		t.Fatalf("Run = %v", err)
	}
	if strings.Join(ran, ",") != "one,two" || !cleaned {
		t.Errorf("ran %v, cleaned %v", ran, cleaned)
	}
	if !strings.Contains(out.String(), "[1/3] one") || !strings.Contains(out.String(), "[2/3] two") {
		t.Errorf("narration:\n%s", out.String())
	}
}

func TestRunRejectsInvalidFlagsFirst(t *testing.T) {
	checked, built := false, false
	s := scenario.Scenario{
		Name:     "demo",
		Validate: func() error { return errors.New("--a must be below --b") },
		Needs: func() []scenario.Need {
			return []scenario.Need{{What: "x", Check: func(context.Context) error { checked = true; return nil }}}
		},
		Steps: func() ([]scenario.Step, func() error) { built = true; return nil, nil },
	}
	rep, _ := reporter()
	err := scenario.Run(t.Context(), s, rep)
	if !errors.Is(err, scenario.ErrUsage) || !strings.Contains(err.Error(), "--a must be below --b") {
		t.Errorf("Run = %v, want a usage error", err)
	}
	if checked || built {
		t.Errorf("checked %v, built %v: nothing may run after a usage error", checked, built)
	}
}

func TestRunStopsAtAFailedNeed(t *testing.T) {
	built := false
	s := scenario.Scenario{
		Name: "demo",
		Needs: func() []scenario.Need {
			return []scenario.Need{{What: "a broker", Check: func(context.Context) error { return errors.New("missing") }}}
		},
		Steps: func() ([]scenario.Step, func() error) { built = true; return nil, nil },
	}
	rep, _ := reporter()
	err := scenario.Run(t.Context(), s, rep)
	if err == nil || !strings.Contains(err.Error(), "need a broker: missing") || built {
		t.Errorf("Run = %v, built = %v", err, built)
	}
	if errors.Is(err, scenario.ErrUsage) {
		t.Error("a failed need is not a usage error")
	}
}
