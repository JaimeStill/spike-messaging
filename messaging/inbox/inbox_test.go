package inbox_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/standards-lab/sqlate/query"
	"github.com/standards-lab/sqlate/sqltest"

	"github.com/JaimeStill/spike-messaging/messaging/inbox"
)

// claim compiles a claim statement from text over sqlate's test dialect, so
// the engine contract is tested without a driver.
func claim(t *testing.T, text string) query.Statement {
	t.Helper()
	fsys := fstest.MapFS{"s/claim.sql": &fstest.MapFile{Data: []byte("--| tier: standard\n" + text)}}
	return query.MustCatalog(query.Patterns()).MustCompile(fsys, "s", sqltest.Dialect{}).Statement("claim")
}

func TestNewAcceptsACompleteEngine(t *testing.T) {
	st := claim(t, "INSERT INTO i (c, s, id) VALUES ({{consumer}}, {{source}}, {{id}})")
	if _, err := inbox.New(inbox.Engine{Claim: st}); err != nil {
		t.Fatal(err)
	}
}

func TestNewRequiresTheStatement(t *testing.T) {
	_, err := inbox.New(inbox.Engine{})
	if err == nil || !strings.Contains(err.Error(), "Claim is not defined") {
		t.Fatalf("New = %v, want the undefined Claim named", err)
	}
}

func TestNewChecksTheStatementsParameters(t *testing.T) {
	st := claim(t, "INSERT INTO i (c, id) VALUES ({{consumer}}, {{id}})")
	_, err := inbox.New(inbox.Engine{Claim: st})
	if err == nil || !strings.Contains(err.Error(), "Claim (claim) takes parameters") {
		t.Fatalf("New = %v, want Claim's parameters refused", err)
	}
}
