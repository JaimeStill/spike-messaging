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
