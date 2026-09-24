package app_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/standards-lab/go-core/process"

	"github.com/JaimeStill/spike-messaging/internal/app"
)

func execute(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errs bytes.Buffer
	a := app.New(&out, &errs)
	a.SetArgs(args)
	return a.Run(t.Context()), out.String(), errs.String()
}

var names = []string{"every", "group", "retry", "permanent", "drain"}

func TestListNamesEveryScenario(t *testing.T) {
	code, out, _ := execute(t, "list")
	if code != process.ExitOK {
		t.Fatalf("exit %d", code)
	}
	for _, name := range names {
		if !strings.Contains(out, "  "+name+" ") {
			t.Errorf("list lacks %q:\n%s", name, out)
		}
	}
}

func TestRootPrintsHelpAndScenarios(t *testing.T) {
	code, out, _ := execute(t)
	if code != process.ExitOK {
		t.Fatalf("exit %d", code)
	}
	for _, want := range []string{"scenario", "list", "--broker", "Scenarios:", "drain"} {
		if !strings.Contains(out, want) {
			t.Errorf("root output lacks %q:\n%s", want, out)
		}
	}
}

func TestScenarioHelpListsItsFlags(t *testing.T) {
	code, out, _ := execute(t, "scenario", "drain", "--help")
	if code != process.ExitOK || !strings.Contains(out, "--grace") || !strings.Contains(out, "--broker") {
		t.Errorf("exit %d:\n%s", code, out)
	}
}

func TestUsageErrors(t *testing.T) {
	cases := map[string]struct {
		args []string
		want string
	}{
		"grace not below drain": {[]string{"scenario", "every", "--grace", "5s", "--drain", "5s"}, "--grace must be below --drain"},
		"unknown flag":          {[]string{"scenario", "every", "--nope"}, "unknown flag"},
		"unknown scenario":      {[]string{"scenario", "nope"}, "unknown command"},
		"unknown broker":        {[]string{"--broker", "nope", "list"}, `unknown broker "nope"`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			code, out, errs := execute(t, c.args...)
			if code != process.ExitUsage || !strings.Contains(errs, c.want) {
				t.Errorf("exit %d, stderr %q, want exit %d mentioning %q", code, errs, process.ExitUsage, c.want)
			}
			if strings.Contains(out, "[1/") || strings.Contains(out, "  every ") {
				t.Errorf("a command ran anyway:\n%s", out)
			}
		})
	}
}

func TestScenarioExitCodes(t *testing.T) {
	code, out, errs := execute(t, "scenario", "retry", "--retry", "20ms")
	if code != process.ExitOK || !strings.Contains(out, "two deliveries") {
		t.Errorf("retry: exit %d\n%s%s", code, out, errs)
	}
	code, _, errs = execute(t, "scenario", "every", "--interval", "10ms", "--work", "1ms", "--ticks", "5", "--fail-after", "2")
	if code != process.ExitFailure || !strings.Contains(errs, "tick 2 failed") {
		t.Errorf("every --fail-after: exit %d, stderr %q", code, errs)
	}
}
