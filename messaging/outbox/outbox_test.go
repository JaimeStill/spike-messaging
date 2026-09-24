package outbox_test

import (
	"testing"

	"github.com/standards-lab/sqlate"

	"github.com/JaimeStill/spike-messaging/core/event"
	"github.com/JaimeStill/spike-messaging/messaging/outbox"
)

// sqlate's *Tx is the transaction a service hands the emitter.
var _ event.Tx = (*sqlate.Tx)(nil)

func TestMigrationsName(t *testing.T) {
	set, err := outbox.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if set.Name != outbox.Source || set.Table != outbox.Table || len(set.Migrations) != 1 {
		t.Fatalf("set = %s/%s with %d migrations", set.Name, set.Table, len(set.Migrations))
	}
}

// The statements the relay alone runs declare their transaction; the two a
// caller runs on its event.Tx leave it to that type.
func TestStatementsDeclareTheirTransaction(t *testing.T) {
	want := map[string]bool{
		"claim_inbox":    false,
		"claim_row":      true,
		"emit":           false,
		"mark_published": true,
	}
	got := map[string]bool{}
	for _, st := range outbox.Statements() {
		got[st.Name()] = st.TransactionRequired()
	}
	if len(got) != len(want) {
		t.Fatalf("statements %v, want %v", got, want)
	}
	for name, required := range want {
		if r, ok := got[name]; !ok || r != required {
			t.Errorf("%s: transaction required = %v (present %v), want %v", name, r, ok, required)
		}
	}
}
