package postgres_test

import (
	"testing"

	"github.com/JaimeStill/spike-messaging/messaging/outbox"
	"github.com/JaimeStill/spike-messaging/messaging/outbox/postgres"
)

// The engine defines every statement with the parameters the outbox names.
func TestEngineIsComplete(t *testing.T) {
	if _, err := outbox.New(postgres.Engine()); err != nil {
		t.Fatal(err)
	}
}

func TestMigrationsName(t *testing.T) {
	set, err := postgres.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if set.Name != postgres.Source || set.Table != postgres.Table || len(set.Migrations) != 1 {
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
	for _, st := range postgres.Statements() {
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
