//go:build integration

package outbox_test

import (
	"errors"
	"testing"

	"github.com/standards-lab/sqlate"

	"github.com/JaimeStill/spike-messaging/core/event"
	"github.com/JaimeStill/spike-messaging/messaging/outbox"
)

// claim claims e for consumer in a transaction of its own, rolling it back
// when rollback is set.
func claim(t *testing.T, db *sqlate.DB, consumer string, e event.Event, rollback bool) bool {
	t.Helper()
	// Transact returns the zero value with an error, so the claim's result
	// leaves the transaction through first.
	undo := errors.New("roll back")
	var first bool
	_, err := sqlate.Transact(t.Context(), db, func(tx *sqlate.Tx) (struct{}, error) {
		var err error
		if first, err = outbox.Claim(t.Context(), tx, consumer, e); err != nil {
			return struct{}{}, err
		}
		if rollback {
			return struct{}{}, undo
		}
		return struct{}{}, nil
	})
	if err != nil && !errors.Is(err, undo) {
		t.Fatalf("claim: %v", err)
	}
	return first
}

func TestClaimIsFirstOnce(t *testing.T) {
	db := migrated(t)
	if !claim(t, db, "billing", tick("1"), false) {
		t.Fatal("the first claim was not first")
	}
	if claim(t, db, "billing", tick("1"), false) {
		t.Fatal("a second claim of a handled event was first")
	}
}

func TestRolledBackClaimIsReleased(t *testing.T) {
	db := migrated(t)
	if !claim(t, db, "billing", tick("1"), true) {
		t.Fatal("the first claim was not first")
	}
	if !claim(t, db, "billing", tick("1"), false) {
		t.Fatal("a claim after a rollback was not first")
	}
}

func TestConsumersClaimIndependently(t *testing.T) {
	db := migrated(t)
	for _, consumer := range []string{"billing", "audit"} {
		if !claim(t, db, consumer, tick("1"), false) {
			t.Fatalf("%s's first claim was not first", consumer)
		}
	}
}

func TestClaimKeysOnSourceAndID(t *testing.T) {
	db := migrated(t)
	other := tick("1")
	other.Source = "/elsewhere"
	claim(t, db, "billing", tick("1"), false)
	if !claim(t, db, "billing", other, false) {
		t.Fatal("an event with the same id from another source was not first")
	}
}

func TestClaimRequiresAConsumer(t *testing.T) {
	db := migrated(t)
	tx, err := db.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := outbox.Claim(t.Context(), tx, "", tick("1")); err == nil {
		t.Fatal("a claim without a consumer succeeded")
	}
}
