package app_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/standards-lab/go-core/process"

	"github.com/JaimeStill/spike-messaging/courier/internal/app"
)

func execute(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errs bytes.Buffer
	a := app.New(&out, &errs)
	a.SetArgs(args)
	return a.Run(t.Context()), out.String(), errs.String()
}

var names = []string{"every", "group", "retry", "permanent", "drain", "outbox", "request", "directives", "assessments", "theater"}

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

func TestListNamesTheBrokersNeed(t *testing.T) {
	const need = "needs NATS with JetStream"
	_, out, _ := execute(t, "list")
	if strings.Contains(out, need) {
		t.Errorf("the memory broker's listing names a NATS need:\n%s", out)
	}
	// Every scenario but every, which builds no broker, needs the server.
	_, out, _ = execute(t, "--broker", "nats", "list")
	if n := strings.Count(out, need); n != len(names)-1 {
		t.Errorf("the nats broker's listing names the NATS need %d times, want %d:\n%s", n, len(names)-1, out)
	}
}

// On memory, the request scenario needs a broker it cannot have: the listing
// names the flag that meets the need, and a run fails on it before any step.
func TestRequestNeedsNATS(t *testing.T) {
	const need = "needs a broker with native request and reply: --broker nats"
	_, out, _ := execute(t, "list")
	if !strings.Contains(out, need) {
		t.Errorf("the memory broker's listing lacks %q:\n%s", need, out)
	}
	_, out, _ = execute(t, "--broker", "nats", "list")
	if strings.Contains(out, need) {
		t.Errorf("the nats broker's listing names the memory broker's need:\n%s", out)
	}
	code, out, errs := execute(t, "scenario", "request")
	if code != process.ExitFailure || !strings.Contains(errs, "request: need a broker with native request and reply: --broker nats: the memory broker has none") {
		t.Errorf("exit %d, stderr %q, want exit %d on the need", code, errs, process.ExitFailure)
	}
	if strings.Contains(out, "[1/") {
		t.Errorf("a step ran before the need was checked:\n%s", out)
	}
}

func TestRootPrintsHelpAndScenarios(t *testing.T) {
	code, out, _ := execute(t)
	if code != process.ExitOK {
		t.Fatalf("exit %d", code)
	}
	for _, want := range []string{"scenario", "list", "--broker", "--nats-url", "Scenarios:", "drain"} {
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
		"unknown broker":        {[]string{"--broker", "nope", "list"}, `unknown broker "nope" (known: memory, nats)`},
		"no outbox events":      {[]string{"scenario", "outbox", "--events", "0"}, "--events must be at least 1"},
		"no requests":           {[]string{"scenario", "request", "--requests", "0"}, "--requests must be at least 1"},
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

// unreachable is a DSN no server answers, which fails fast.
const unreachable = "postgres://nobody@127.0.0.1:1/x?sslmode=disable&connect_timeout=1"

func TestOutboxFailsOnItsNeedFirst(t *testing.T) {
	code, out, errs := execute(t, "scenario", "outbox", "--dsn", unreachable)
	if code != process.ExitFailure || !strings.Contains(errs, "outbox: need Postgres") {
		t.Errorf("exit %d, stderr %q, want exit %d on the Postgres need", code, errs, process.ExitFailure)
	}
	if strings.Contains(out, "[1/") {
		t.Errorf("a step ran before the need was checked:\n%s", out)
	}
}

func TestOnlyTheOutboxNeedsPostgres(t *testing.T) {
	code, out, _ := execute(t, "--dsn", unreachable, "scenario", "retry", "--retry", "20ms")
	if code != process.ExitOK {
		t.Errorf("retry with an unreachable --dsn: exit %d\n%s", code, out)
	}
}

func TestNATSNeedsAURL(t *testing.T) {
	t.Setenv("MESSAGING_NATS_URL", "")
	code, out, errs := execute(t, "--broker", "nats", "scenario", "retry")
	if code != process.ExitFailure || !strings.Contains(errs, "retry: need NATS with JetStream") || !strings.Contains(errs, "no NATS URL") {
		t.Errorf("exit %d, stderr %q, want exit %d on the NATS need", code, errs, process.ExitFailure)
	}
	if strings.Contains(out, "[1/") {
		t.Errorf("a step ran before the need was checked:\n%s", out)
	}
}

func TestNATSFailsOnAnUnreachableServer(t *testing.T) {
	code, out, errs := execute(t, "--broker", "nats", "--nats-url", "nats://127.0.0.1:1", "scenario", "group")
	if code != process.ExitFailure || !strings.Contains(errs, "group: need NATS with JetStream") {
		t.Errorf("exit %d, stderr %q, want exit %d on the NATS need", code, errs, process.ExitFailure)
	}
	if strings.Contains(out, "[1/") {
		t.Errorf("a step ran before the need was checked:\n%s", out)
	}
}
