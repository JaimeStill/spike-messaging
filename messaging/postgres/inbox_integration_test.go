//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/standards-lab/sqlate"

	"github.com/JaimeStill/spike-messaging/core/event"
	"github.com/JaimeStill/spike-messaging/core/reactor"
	"github.com/JaimeStill/spike-messaging/messaging"
	"github.com/JaimeStill/spike-messaging/messaging/memory"
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
		if first, err = in.Claim(t.Context(), tx, consumer, e); err != nil {
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
	if _, err := in.Claim(t.Context(), tx, "", tick("1")); err == nil {
		t.Fatal("a claim without a consumer succeeded")
	}
}

// A handler that outlives AckWait holds its claim only until its context's
// deadline, which is the delivery's AckWait: the deadline rolls its
// transaction back, so the redelivery, to another member of the group,
// claims the event and does the work once. The redelivery's claim waits on
// the first handler's row lock rather than failing, and under READ
// COMMITTED proceeds once the lock is released.
func TestClaimAcrossAckWait(t *testing.T) {
	db := migrated(t)
	b := memory.New()
	sub := messaging.Subscription{Name: "billing", AckWait: 200 * time.Millisecond}
	var mu sync.Mutex
	var attempts, work int
	firsts := make([]bool, 0, 2)
	handler := func(ctx context.Context, e event.Event) error {
		mu.Lock()
		attempts++
		n := attempts
		mu.Unlock()
		_, err := sqlate.Transact(ctx, db, func(tx *sqlate.Tx) (struct{}, error) {
			first, err := in.Claim(ctx, tx, sub.Name, e)
			if err != nil {
				return struct{}{}, err
			}
			mu.Lock()
			firsts = append(firsts, first)
			mu.Unlock()
			if !first {
				return struct{}{}, nil
			}
			if n == 1 {
				<-ctx.Done() // overrun AckWait, holding the claim
				return struct{}{}, ctx.Err()
			}
			mu.Lock()
			work++
			mu.Unlock()
			return struct{}{}, nil
		})
		return err
	}
	for range 2 {
		src, err := b.Subscribe(sub)
		if err != nil {
			t.Fatal(err)
		}
		r := reactor.New(src, handler)
		if err := r.Start(t.Context()); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { stop(r) })
		eventually(t, "the member to be receiving", r.Ready)
	}
	if err := b.Publish(t.Context(), tick("1")); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the redelivery to do the work", func() bool { mu.Lock(); defer mu.Unlock(); return work == 1 })
	time.Sleep(2 * sub.AckWait)
	mu.Lock()
	defer mu.Unlock()
	if attempts != 2 || work != 1 {
		t.Errorf("%d deliveries did the work %d times, want 2 deliveries and the work once", attempts, work)
	}
	if !slices.Equal(firsts, []bool{true, true}) {
		t.Errorf("claims were first %v, want both first: the overrun's rollback releases its claim", firsts)
	}
	if claim(t, db, sub.Name, tick("1"), false) {
		t.Error("the event was claimable after the redelivery committed")
	}
}
