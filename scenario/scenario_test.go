package scenario_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/JaimeStill/spike-messaging/output"
	"github.com/JaimeStill/spike-messaging/scenario"
)

func reporter() (*scenario.Reporter, *bytes.Buffer) {
	var out bytes.Buffer
	return scenario.NewReporter(output.New(&out, &bytes.Buffer{})), &out
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
		Needs:    []scenario.Need{{What: "x", Check: func(context.Context) error { checked = true; return nil }}},
		Steps:    func() ([]scenario.Step, func() error) { built = true; return nil, nil },
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
		Name:  "demo",
		Needs: []scenario.Need{{What: "a broker", Check: func(context.Context) error { return errors.New("missing") }}},
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
