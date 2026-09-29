//go:build integration

package exercise_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/JaimeStill/spike-messaging/core/event"
	"github.com/JaimeStill/spike-messaging/services/exercise/data"
	"github.com/JaimeStill/spike-messaging/services/exercise/domain/exercise"
	"github.com/JaimeStill/spike-messaging/services/exercise/domain/exercise/rules"
)

// holdRound stands in for a resolution in flight: it locks the exercise's
// row in a transaction of its own and advances its round, and returns the
// function that commits it.
func holdRound(t *testing.T, db *data.Database, id string) (commit func()) {
	t.Helper()
	tx, err := db.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(t.Context(), `SELECT 1 FROM exercise WHERE id = $1 FOR UPDATE`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(t.Context(), `UPDATE exercise SET round = round + 1 WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	done := false
	t.Cleanup(func() {
		if !done {
			_ = tx.Rollback()
		}
	})
	return func() {
		done = true
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
}

// waits reports whether fn is still blocked after a pause, then releases it
// and returns its result.
func waits[T any](t *testing.T, release func(), fn func() T) (blocked bool, result T) {
	t.Helper()
	out := make(chan T, 1)
	go func() { out <- fn() }()
	select {
	case r := <-out:
		release()
		return false, r
	case <-time.After(200 * time.Millisecond):
	}
	release()
	select {
	case r := <-out:
		return true, r
	case <-time.After(5 * time.Second):
		t.Fatal("the command never finished after the lock was released")
	}
	return true, result
}

// A stop that races a resolution waits for it, and concludes at the round
// the resolution left rather than the round the stop first saw.
func TestStopWaitsOnAResolutionInFlight(t *testing.T) {
	svc, db := setup(t)
	ex := started(t, svc, func(c *exercise.CreateExercise) { c.RoundInterval = "1h" })
	commit := holdRound(t, db, ex.ID)

	blocked, err := waits(t, commit, func() error {
		_, err := svc.Stop(context.Background(), ex.ID)
		return err
	})
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
	if !blocked {
		t.Error("the stop did not wait on the row lock")
	}
	rows := outboxRows(t, db)
	last := rows[len(rows)-1]
	var d exercise.ConcludedData
	if err := json.Unmarshal(last.Data, &d); err != nil {
		t.Fatal(err)
	}
	if last.Type != "exercise.concluded" || d.Round != 1 || d.Reason != "stopped" {
		t.Errorf("last event = %s %+v, want concluded at round 1, stopped", last.Type, d)
	}
}

// Orders recorded while a resolution holds the exercise wait for it, then
// judge their round against the round it left: an order for the round just
// resolved is refused permanently.
func TestRecordOrdersWaitsOnAResolutionInFlight(t *testing.T) {
	svc, db := setup(t)
	ex := started(t, svc, func(c *exercise.CreateExercise) { c.RoundInterval = "1h" })
	commit := holdRound(t, db, ex.ID)

	cmd := exercise.RecordOrders{Exercise: ex.ID, Faction: "red", Round: 1, Orders: []rules.Order{{Element: "r1"}}}
	blocked, err := waits(t, commit, func() error {
		return svc.RecordOrders(context.Background(), cmd, nil)
	})
	if !blocked {
		t.Error("the orders did not wait on the row lock")
	}
	if !event.IsPermanent(err) {
		t.Errorf("orders for the round the resolution left = %v, want a permanent refusal", err)
	}
}

// Resolvers running at once, as replicas do, resolve a due round once:
// each exercise is locked with SKIP LOCKED and re-checked as due.
func TestConcurrentResolversResolveARoundOnce(t *testing.T) {
	svc, db := setup(t)
	ex := started(t, svc, func(c *exercise.CreateExercise) { c.RoundInterval = "50ms" })
	time.Sleep(80 * time.Millisecond)

	var wg sync.WaitGroup
	counts := make([]int, 8)
	for i := range counts {
		wg.Go(func() {
			n, err := svc.ResolveDue(context.Background())
			if err != nil {
				t.Errorf("resolve due: %v", err)
			}
			counts[i] = n
		})
	}
	wg.Wait()
	total := 0
	for _, n := range counts {
		total += n
	}
	if total != 1 {
		t.Errorf("resolvers resolved %d rounds in all, want 1", total)
	}
	if got := find(t, svc, ex.ID).Round; got != 1 {
		t.Errorf("round = %d, want 1", got)
	}
	if n := scalar[int](t, db, `SELECT count(*) FROM exercise_round WHERE exercise_id = $1`, ex.ID); n != 2 {
		t.Errorf("history holds %d rounds, want rounds 0 and 1", n)
	}
}
