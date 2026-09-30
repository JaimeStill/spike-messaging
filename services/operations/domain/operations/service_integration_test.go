//go:build integration

package operations_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"testing"
	"uuid"

	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/migrate"

	"github.com/JaimeStill/spike-messaging/core/event"
	"github.com/JaimeStill/spike-messaging/messaging/inbox"
	"github.com/JaimeStill/spike-messaging/messaging/outbox"
	"github.com/JaimeStill/spike-messaging/messaging/postgres"
	"github.com/JaimeStill/spike-messaging/services/operations/data"
	"github.com/JaimeStill/spike-messaging/services/operations/domain/operations"
	"github.com/JaimeStill/spike-messaging/services/operations/domain/operations/route"
	"github.com/JaimeStill/spike-messaging/services/operations/internal/pgtest"
)

// ob and in are the outbox and the inbox on the Postgres engine, as the
// composition root builds them.
var (
	ob = must(outbox.New(postgres.Outbox()))
	in = must(inbox.New(postgres.Inbox()))
)

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// setup returns the service over a throwaway, migrated database, with the
// real recorder over the outbox's sink, and the database for the test's
// own reads.
func setup(t *testing.T) (*operations.Service, *data.Database) {
	t.Helper()
	db := pgtest.Open(t)
	svc := operations.New(db, event.NewRecorder(ob.Sink(), "/operations"))
	if err := svc.Verify(t.Context()); err != nil {
		t.Fatalf("verify: %v", err)
	}
	return svc, db
}

func loc(x, y int) route.Location {
	return route.Location{Sector: "a", Point: route.Point{X: x, Y: y}}
}

// The fixture is one 5×1 sector "a" with its objective at 4,0, and red's
// squad r1 at 0,0 and scout r2 at 1,0, both ready.
func open(t *testing.T, svc *operations.Service) string {
	t.Helper()
	id := uuid.NewV7().String()
	err := svc.Open(t.Context(), operations.Open{
		Exercise: id,
		Map: route.Map{Sectors: []route.Sector{{
			ID: "a", Width: 5, Height: 1,
		}}},
		Factions:   [2]string{"red", "blue"},
		RoundLimit: 3,
	}, nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return id
}

func red(id string, round int, r1, r2 route.Location) operations.Maneuver {
	return operations.Maneuver{Exercise: id, Faction: "red", Round: round, Own: []route.Element{
		{ID: "r1", Kind: "squad", Status: route.StatusReady, At: r1},
		{ID: "r2", Kind: "scout", Status: route.StatusReady, At: r2},
	}}
}

// assign returns red's directives on round: each element's target, under
// the secure rule.
func assign(id string, round int, targets map[string]*route.Location) operations.Assign {
	c := operations.Assign{Exercise: id, Faction: "red", Round: round, Sequence: round + 1}
	for _, e := range slices.Sorted(func(yield func(string) bool) {
		for k := range targets {
			if !yield(k) {
				return
			}
		}
	}) {
		c.Directives = append(c.Directives, operations.Directive{Element: e, Rule: "secure", Target: targets[e]})
	}
	return c
}

// ordersRows returns the orders the outbox holds, in the order they were
// written.
func ordersRows(t *testing.T, db *data.Database) []operations.OrdersData {
	t.Helper()
	r, err := db.QueryContext(t.Context(), `SELECT header, data FROM messaging_outbox ORDER BY seq`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	var out []operations.OrdersData
	for r.Next() {
		var header, body []byte
		if err := r.Scan(&header, &body); err != nil {
			t.Fatal(err)
		}
		var h event.Header
		if err := json.Unmarshal(header, &h); err != nil {
			t.Fatal(err)
		}
		e, err := event.Decode(h, body)
		if err != nil {
			t.Fatal(err)
		}
		if e.Type != operations.OrdersIssued.Type() {
			t.Fatalf("outbox holds a %s event", e.Type)
		}
		var d operations.OrdersData
		if err := json.Unmarshal(e.Data, &d); err != nil {
			t.Fatal(err)
		}
		if e.Subject != d.Exercise {
			t.Errorf("subject %s, want the exercise %s", e.Subject, d.Exercise)
		}
		out = append(out, d)
	}
	if err := r.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func steps(ls ...route.Location) []route.Location { return ls }

func sameOrders(a, b []route.Order) bool {
	return slices.EqualFunc(a, b, func(x, y route.Order) bool {
		return x.Element == y.Element && x.Retreat == y.Retreat && x.Pursue == y.Pursue && slices.Equal(x.Steps, y.Steps)
	})
}

// A round's observation issues the next round's orders, none before any
// directive; a directive re-issues that round's orders along the new plan;
// the next observation issues the orders after it.
func TestManeuverIssuesOrdersTowardTargets(t *testing.T) {
	svc, db := setup(t)
	id := open(t, svc)
	ctx := t.Context()

	if err := svc.Maneuver(ctx, red(id, 0, loc(0, 0), loc(1, 0)), nil); err != nil {
		t.Fatal(err)
	}
	objective := loc(4, 0)
	if err := svc.Assign(ctx, assign(id, 0, map[string]*route.Location{"r1": &objective, "r2": &objective}), nil); err != nil {
		t.Fatal(err)
	}
	if err := svc.Maneuver(ctx, red(id, 1, loc(1, 0), loc(3, 0)), nil); err != nil {
		t.Fatal(err)
	}

	got := ordersRows(t, db)
	want := []operations.OrdersData{
		{Exercise: id, Faction: "red", Round: 1, Orders: []route.Order{}},
		// r1 follows r2 into 1,0, the cell r2 leaves.
		{Exercise: id, Faction: "red", Round: 1, Orders: []route.Order{
			{Element: "r1", Steps: steps(loc(1, 0))},
			{Element: "r2", Steps: steps(loc(2, 0), loc(3, 0))},
		}},
		{Exercise: id, Faction: "red", Round: 2, Orders: []route.Order{
			{Element: "r1", Steps: steps(loc(2, 0))},
			{Element: "r2", Steps: steps(loc(4, 0))},
		}},
	}
	if len(got) != len(want) {
		t.Fatalf("outbox holds %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i].Round != want[i].Round || got[i].Faction != want[i].Faction || !sameOrders(got[i].Orders, want[i].Orders) {
			t.Errorf("orders %d = %+v, want %+v", i, got[i], want[i])
		}
	}

	ops, err := svc.Find(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 2 || ops[1].Faction != "red" || ops[1].LastRound != 1 || len(ops[1].Standing) != 2 || ops[1].Standing["r2"].Rule != "secure" || ops[0].LastRound != -1 {
		t.Errorf("Find = %+v", ops)
	}
}

// An observation of a round before the last one acted on changes nothing,
// and so does a directive older than the last one applied; a directive
// that leaves the plan as it was re-issues nothing.
func TestStaleInputsAreSkipped(t *testing.T) {
	svc, db := setup(t)
	id := open(t, svc)
	ctx := t.Context()
	for _, round := range []int{1, 0, 1} {
		if err := svc.Maneuver(ctx, red(id, round, loc(0, 0), loc(1, 0)), nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.Assign(ctx, assign(id, 1, map[string]*route.Location{"r1": nil}), nil); err != nil {
		t.Fatal(err)
	}
	target := loc(4, 0)
	if err := svc.Assign(ctx, assign(id, 0, map[string]*route.Location{"r2": &target}), nil); err != nil {
		t.Fatal(err)
	}
	if got := ordersRows(t, db); len(got) != 1 || got[0].Round != 2 {
		t.Errorf("outbox holds %+v, want round 2's orders alone", got)
	}
	ops, err := svc.Find(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range ops {
		if op.Faction == "red" && (len(op.Standing) != 0 || op.DirectiveSeq != 2) {
			t.Errorf("red's operation = %+v, want no standing and directive sequence 2", op)
		}
	}
}

// A directive of the same round but a lower sequence, which arrives behind
// a newer one, changes nothing.
func TestALateLowerSequenceIsSkipped(t *testing.T) {
	svc, db := setup(t)
	id := open(t, svc)
	ctx := t.Context()
	if err := svc.Maneuver(ctx, red(id, 0, loc(0, 0), loc(1, 0)), nil); err != nil {
		t.Fatal(err)
	}
	newer, older := loc(4, 0), loc(2, 0)
	second := assign(id, 0, map[string]*route.Location{"r2": &newer})
	second.Sequence = 2
	first := assign(id, 0, map[string]*route.Location{"r2": &older, "r1": &older})
	first.Sequence = 1
	for _, c := range []operations.Assign{second, first} {
		if err := svc.Assign(ctx, c, nil); err != nil {
			t.Fatal(err)
		}
	}
	if got := ordersRows(t, db); len(got) != 2 {
		t.Errorf("outbox holds %+v, want round 1's orders and their one re-issue", got)
	}
	ops, err := svc.Find(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range ops {
		if op.Faction == "red" && (op.DirectiveSeq != 2 || len(op.Standing) != 1 || op.Standing["r2"].Target != newer) {
			t.Errorf("red's operation = %+v, want the sequence-2 directive's target alone", op)
		}
	}
}

// A directive that arrives after the operation has acted on a later
// round, as when command catches up after an outage, still sets the
// targets: command sends nothing more while its decisions stand, so
// skipping it would lose them. It re-issues nothing, since the orders out
// are for a later round, and the next observation plans with its targets.
func TestALateDirectiveStillTakesEffect(t *testing.T) {
	svc, db := setup(t)
	id := open(t, svc)
	ctx := t.Context()
	for _, round := range []int{0, 1} {
		if err := svc.Maneuver(ctx, red(id, round, loc(0, 0), loc(1, 0)), nil); err != nil {
			t.Fatal(err)
		}
	}
	target := loc(4, 0)
	if err := svc.Assign(ctx, assign(id, 0, map[string]*route.Location{"r2": &target}), nil); err != nil {
		t.Fatal(err)
	}
	if got := ordersRows(t, db); len(got) != 2 {
		t.Fatalf("outbox holds %+v, want rounds 1 and 2's orders alone", got)
	}
	if err := svc.Maneuver(ctx, red(id, 2, loc(0, 0), loc(1, 0)), nil); err != nil {
		t.Fatal(err)
	}
	got := ordersRows(t, db)
	want := []route.Order{{Element: "r2", Steps: steps(loc(2, 0), loc(3, 0))}}
	if len(got) != 3 || got[2].Round != 3 || !sameOrders(got[2].Orders, want) {
		t.Errorf("outbox holds %+v, want round 3's orders toward the late directive's target", got)
	}
}

// A directive that overtakes its round's observation sets the targets and
// raises nothing, because the orders out are for a round already resolved;
// the observation then plans with them.
func TestADirectiveAheadOfItsObservationWaitsForIt(t *testing.T) {
	svc, db := setup(t)
	id := open(t, svc)
	ctx := t.Context()
	if err := svc.Maneuver(ctx, red(id, 0, loc(0, 0), loc(1, 0)), nil); err != nil {
		t.Fatal(err)
	}
	target := loc(4, 0)
	if err := svc.Assign(ctx, assign(id, 1, map[string]*route.Location{"r2": &target}), nil); err != nil {
		t.Fatal(err)
	}
	if got := ordersRows(t, db); len(got) != 1 || got[0].Round != 1 {
		t.Fatalf("outbox holds %+v, want round 1's orders alone", got)
	}
	if err := svc.Maneuver(ctx, red(id, 1, loc(0, 0), loc(1, 0)), nil); err != nil {
		t.Fatal(err)
	}
	got := ordersRows(t, db)
	want := []route.Order{{Element: "r2", Steps: steps(loc(2, 0), loc(3, 0))}}
	if len(got) != 2 || got[1].Round != 2 || !sameOrders(got[1].Orders, want) {
		t.Errorf("outbox holds %+v, want round 2's orders toward the directive's target", got)
	}
}

// A destroyed element's target and rule are dropped with it.
func TestDestroyedElementsDropTheirTargets(t *testing.T) {
	svc, _ := setup(t)
	id := open(t, svc)
	ctx := t.Context()
	target := loc(4, 0)
	if err := svc.Maneuver(ctx, red(id, 0, loc(0, 0), loc(1, 0)), nil); err != nil {
		t.Fatal(err)
	}
	if err := svc.Assign(ctx, assign(id, 0, map[string]*route.Location{"r1": &target, "r2": &target}), nil); err != nil {
		t.Fatal(err)
	}
	c := red(id, 1, loc(0, 0), loc(3, 0))
	c.Own = c.Own[1:]
	if err := svc.Maneuver(ctx, c, nil); err != nil {
		t.Fatal(err)
	}
	ops, err := svc.Find(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, standing := ops[1].Standing["r1"]; standing || len(ops[1].Elements) != 1 {
		t.Errorf("red = %+v, want r1 and its target and rule gone", ops[1])
	}
}

// An engaged element stays in its fight under an engage directive; a
// retreat directive on the same round changes the plan, so the round's
// orders are issued again, with the retreat's one step flagged.
func TestARetreatWithdrawsAnEngagedElement(t *testing.T) {
	svc, db := setup(t)
	id := open(t, svc)
	ctx := t.Context()
	c := red(id, 0, loc(1, 0), loc(3, 0))
	c.Own[0].Status = route.StatusEngaged
	if err := svc.Maneuver(ctx, c, nil); err != nil {
		t.Fatal(err)
	}
	fight, back := loc(1, 0), loc(0, 0)
	engage := operations.Assign{Exercise: id, Faction: "red", Round: 0, Sequence: 1, Directives: []operations.Directive{
		{Element: "r1", Rule: "engage", Target: &fight},
	}}
	if err := svc.Assign(ctx, engage, nil); err != nil {
		t.Fatal(err)
	}
	retreat := engage
	retreat.Sequence = 2
	retreat.Directives = []operations.Directive{{Element: "r1", Rule: "retreat", Target: &back}}
	if err := svc.Assign(ctx, retreat, nil); err != nil {
		t.Fatal(err)
	}
	got := ordersRows(t, db)
	want := []route.Order{{Element: "r1", Steps: steps(back), Retreat: true}}
	if len(got) != 2 || len(got[0].Orders) != 0 || got[1].Round != 1 || !sameOrders(got[1].Orders, want) {
		t.Errorf("outbox holds %+v, want round 1's empty orders, then r1's retreat", got)
	}
	ops, err := svc.Find(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if st := ops[1].Standing["r1"]; st.Rule != "retreat" || st.Target != back {
		t.Errorf("red = %+v, want r1's retreat to 0,0", ops[1])
	}
}

// No orders are issued for a round past the exercise's round limit.
func TestNoOrdersPastTheRoundLimit(t *testing.T) {
	svc, db := setup(t)
	id := open(t, svc)
	for _, round := range []int{2, 3} {
		if err := svc.Maneuver(t.Context(), red(id, round, loc(0, 0), loc(1, 0)), nil); err != nil {
			t.Fatal(err)
		}
	}
	if got := ordersRows(t, db); len(got) != 1 || got[0].Round != 3 {
		t.Errorf("outbox holds %+v, want round 3's orders alone", got)
	}
}

// An input for an operation not open yet fails, not permanently, and rolls
// its claim back, so its redelivery after the start is handled.
func TestInputsBeforeOpenAreRedelivered(t *testing.T) {
	svc, db := setup(t)
	id := uuid.NewV7().String()
	e := event.Event{ID: uuid.NewV7().String(), Source: "/exercise", Type: "exercise.round.observed"}
	claim := func(ctx context.Context, tx *sqlate.Tx) (bool, error) {
		return in.Claim(ctx, tx, "operations-observed", e)
	}

	for _, err := range []error{
		svc.Maneuver(t.Context(), red(id, 0, loc(0, 0), loc(1, 0)), claim),
		svc.Assign(t.Context(), assign(id, 0, nil), nil),
		svc.Close(t.Context(), operations.Close{Exercise: id}, nil),
	} {
		if !errors.Is(err, operations.ErrNotOpen) || event.IsPermanent(err) {
			t.Errorf("err = %v, want ErrNotOpen, not permanent", err)
		}
	}
	if _, err := svc.Find(t.Context(), id); !errors.Is(err, operations.ErrNotFound) {
		t.Errorf("Find = %v, want ErrNotFound", err)
	}

	err := svc.Open(t.Context(), operations.Open{
		Exercise: id, Map: route.Map{Sectors: []route.Sector{{ID: "a", Width: 5, Height: 1}}},
		Factions: [2]string{"red", "blue"}, RoundLimit: 3,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Maneuver(t.Context(), red(id, 0, loc(0, 0), loc(1, 0)), claim); err != nil {
		t.Fatal(err)
	}
	if got := ordersRows(t, db); len(got) != 1 {
		t.Errorf("outbox holds %+v, want the redelivered observation's orders", got)
	}
}

// A redelivered event, claimed again, changes nothing.
func TestAClaimedRepeatChangesNothing(t *testing.T) {
	svc, db := setup(t)
	id := open(t, svc)
	e := event.Event{ID: uuid.NewV7().String(), Source: "/exercise", Type: "exercise.round.observed"}
	claim := func(ctx context.Context, tx *sqlate.Tx) (bool, error) {
		return in.Claim(ctx, tx, "operations-observed", e)
	}
	for range 2 {
		c := red(id, 0, loc(0, 0), loc(1, 0))
		if err := svc.Maneuver(t.Context(), c, claim); err != nil {
			t.Fatal(err)
		}
		// A repeat of a round would be skipped anyway; the claim must stop
		// it first, so reset the round the operation acted on.
		if _, err := db.ExecContext(t.Context(), `UPDATE operation SET last_round = -1`); err != nil {
			t.Fatal(err)
		}
	}
	if got := ordersRows(t, db); len(got) != 1 {
		t.Errorf("outbox holds %d orders events, want 1", len(got))
	}
}

// A closed operation acts on nothing more.
func TestCloseEndsTheOperation(t *testing.T) {
	svc, db := setup(t)
	id := open(t, svc)
	if err := svc.Close(t.Context(), operations.Close{Exercise: id}, nil); err != nil {
		t.Fatal(err)
	}
	if err := svc.Maneuver(t.Context(), red(id, 0, loc(0, 0), loc(1, 0)), nil); err != nil {
		t.Fatal(err)
	}
	if got := ordersRows(t, db); len(got) != 0 {
		t.Errorf("a closed operation issued %+v", got)
	}
	ops, err := svc.Find(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range ops {
		if op.Status != operations.StatusClosed {
			t.Errorf("%s is %s", op.Faction, op.Status)
		}
	}
}

// Two replicas handling one round's observation at once issue its orders
// once: the row lock orders them, and the second skips the round.
func TestConcurrentManeuversIssueOnce(t *testing.T) {
	svc, db := setup(t)
	id := open(t, svc)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Go(func() { errs <- svc.Maneuver(context.Background(), red(id, 0, loc(0, 0), loc(1, 0)), nil) })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := ordersRows(t, db); len(got) != 1 {
		t.Errorf("outbox holds %d orders events, want 1", len(got))
	}
}

func TestMigrationsUpAndDown(t *testing.T) {
	db := pgtest.OpenBare(t)
	messaging, err := postgres.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	m, err := migrate.New(db.DB, data.Migrations(messaging), migrate.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Up(t.Context()); err != nil {
		t.Fatalf("up: %v", err)
	}
	// exists reports whether the query's one boolean holds.
	exists := func(q string) bool {
		t.Helper()
		r, err := db.QueryContext(t.Context(), q)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = r.Close() }()
		var ok bool
		if !r.Next() || r.Scan(&ok) != nil {
			t.Fatalf("%s: no boolean", q)
		}
		return ok
	}
	if exists(`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'operation' AND column_name = 'directive_round')`) {
		t.Error("after up, the directive_round column remains")
	}
	if err := m.Down(t.Context(), 1); err != nil {
		t.Fatalf("down directive round: %v", err)
	}
	if !exists(`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'operation' AND column_name = 'directive_round')`) {
		t.Error("after 0005's down, the directive_round column is missing")
	}
	if err := m.Down(t.Context(), 1); err != nil {
		t.Fatalf("down directive sequence: %v", err)
	}
	if exists(`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'operation' AND column_name = 'directive_sequence')`) {
		t.Error("after 0004's down, the directive_sequence column remains")
	}
	if err := m.Down(t.Context(), 1); err != nil {
		t.Fatalf("down rules: %v", err)
	}
	if exists(`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'operation' AND column_name = 'rules')`) {
		t.Error("after 0003's down, the rules column remains")
	}
	if err := m.Down(t.Context(), 2); err != nil {
		t.Fatalf("down: %v", err)
	}
	if exists(`SELECT to_regclass('operation') IS NOT NULL`) {
		t.Error("after down, the operation table remains")
	}
}
