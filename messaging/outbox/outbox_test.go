package outbox_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/query"
	"github.com/standards-lab/sqlate/sqltest"

	"github.com/JaimeStill/spike-messaging/core/event"
	"github.com/JaimeStill/spike-messaging/messaging/outbox"
)

// sqlate's *Tx is the transaction a service hands the emitter.
var _ event.Tx = (*sqlate.Tx)(nil)

// compile builds statements from text keyed by name, over sqlate's test
// dialect, so the engine contract is tested without a driver.
func compile(t *testing.T, files map[string]string) *query.Statements {
	t.Helper()
	fsys := fstest.MapFS{}
	for name, text := range files {
		fsys["s/"+name+".sql"] = &fstest.MapFile{Data: []byte("--| tier: standard\n" + text)}
	}
	return query.MustCatalog(query.Patterns()).MustCompile(fsys, "s", sqltest.Dialect{})
}

func engine(t *testing.T, emit string) outbox.Engine {
	t.Helper()
	s := compile(t, map[string]string{
		"emit":           emit,
		"claim_row":      "SELECT seq, header, data FROM o",
		"mark_published": "UPDATE o SET p = 1 WHERE seq = {{seq}}",
		"claim_inbox":    "INSERT INTO i (c, s, id) VALUES ({{consumer}}, {{source}}, {{id}})",
	})
	return outbox.Engine{
		Emit:          s.Statement("emit"),
		ClaimRow:      s.Statement("claim_row"),
		MarkPublished: s.Statement("mark_published"),
		ClaimInbox:    s.Statement("claim_inbox"),
	}
}

const emit = "INSERT INTO o (s, id, h, d) VALUES ({{source}}, {{id}}, {{header}}, {{data}})"

func TestNewAcceptsACompleteEngine(t *testing.T) {
	if _, err := outbox.New(engine(t, emit)); err != nil {
		t.Fatal(err)
	}
}

func TestNewRequiresEveryStatement(t *testing.T) {
	_, err := outbox.New(outbox.Engine{})
	if err == nil {
		t.Fatal("New accepted an engine with no statements")
	}
	for _, field := range []string{"Emit", "ClaimRow", "MarkPublished", "ClaimInbox"} {
		if !strings.Contains(err.Error(), field+" is not defined") {
			t.Errorf("error %q does not name the undefined %s", err, field)
		}
	}
}

func TestNewChecksEachStatementsParameters(t *testing.T) {
	missing := "INSERT INTO o (s, id, h) VALUES ({{source}}, {{id}}, {{header}})"
	_, err := outbox.New(engine(t, missing))
	if err == nil || !strings.Contains(err.Error(), "Emit (emit) takes parameters") {
		t.Fatalf("New = %v, want Emit's parameters refused", err)
	}
}

func TestNewRejectsARequiredTransactionOnTheCallersStatements(t *testing.T) {
	_, err := outbox.New(engine(t, "--| transaction: required\n"+emit))
	if err == nil || !strings.Contains(err.Error(), "Emit (emit) declares a transaction required") {
		t.Fatalf("New = %v, want Emit's transaction declaration refused", err)
	}
}
