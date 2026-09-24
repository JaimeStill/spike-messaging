package scenario_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/JaimeStill/spike-messaging/messaging"
	"github.com/JaimeStill/spike-messaging/messaging/memory"
	"github.com/JaimeStill/spike-messaging/scenario"
)

func memoryBrokers() (messaging.Broker, error) { return memory.New(), nil }

// execute runs the named scenario's command with args on the memory broker.
func execute(t *testing.T, name string, args ...string) (string, error) {
	t.Helper()
	for _, s := range scenario.Scenarios(memoryBrokers, nil) {
		if s.Name != name {
			continue
		}
		rep, out := reporter()
		cmd := scenario.Command(s, func() *scenario.Reporter { return rep })
		cmd.SetArgs(args)
		err := cmd.ExecuteContext(t.Context())
		return out.String(), err
	}
	t.Fatalf("no scenario %q", name)
	return "", nil
}

func TestScenariosOnMemory(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want []string
	}{
		{"every", []string{"--interval", "20ms", "--work", "5ms"}, []string{"tick 3 done", "drained cleanly", "3 ticks handled"}},
		{"group", []string{"--events", "8", "--interval", "5ms", "--work", "20ms"}, []string{"worker-a handled", "worker-b handled", "drained cleanly"}},
		{"retry", []string{"--retry", "50ms"}, []string{"attempt 1 failed", "attempt 2 handled", "two deliveries"}},
		{"permanent", []string{"--quiet", "150ms"}, []string{"fails permanently", "event 2 handled", "delivered once and terminated"}},
		{"drain", []string{"--work", "50ms"}, []string{"handling finished", "drained cleanly", "acknowledgement held"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := execute(t, c.name, c.args...)
			if err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
			for _, w := range c.want {
				if !strings.Contains(out, w) {
					t.Errorf("narration lacks %q:\n%s", w, out)
				}
			}
		})
	}
}

func TestEveryFailureEndsTheRun(t *testing.T) {
	out, err := execute(t, "every", "--interval", "10ms", "--work", "1ms", "--ticks", "5", "--fail-after", "2")
	if err == nil || !strings.Contains(err.Error(), "tick 2 failed") {
		t.Fatalf("err = %v\n%s", err, out)
	}
	if errors.Is(err, scenario.ErrUsage) {
		t.Error("a runtime failure is not a usage error")
	}
}

func TestDrainCancelsAfterGrace(t *testing.T) {
	out, err := execute(t, "drain", "--work", "5s", "--grace", "100ms", "--drain", "2s")
	if err == nil || !strings.Contains(err.Error(), "worker: reactor: handlers cancelled after grace 100ms") {
		t.Fatalf("err = %v\n%s", err, out)
	}
	if !strings.Contains(out, "handling cancelled") {
		t.Errorf("narration lacks the cancellation:\n%s", out)
	}
}

func TestGraceBelowDrainIsUsage(t *testing.T) {
	for _, name := range []string{"every", "drain"} {
		out, err := execute(t, name, "--grace", "5s", "--drain", "5s")
		if !errors.Is(err, scenario.ErrUsage) {
			t.Errorf("%s: err = %v, want a usage error", name, err)
		}
		if strings.Contains(out, "[1/") {
			t.Errorf("%s: a step ran before the usage error:\n%s", name, out)
		}
	}
}
