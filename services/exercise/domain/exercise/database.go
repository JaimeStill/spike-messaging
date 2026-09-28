package exercise

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/query"

	"github.com/JaimeStill/spike-messaging/services/exercise/data"
	"github.com/JaimeStill/spike-messaging/services/exercise/domain/exercise/rules"
)

//go:embed statements/*.sql
var files embed.FS

// store is the domain's SQL client: the statements of statements/ bound
// once to their typed handles, and the operations as methods named for
// them. It is the package's sole importer of the query library, and the one
// place the rules' values become JSON for their jsonb columns and back.
// Every method that writes takes the command's transaction.
type store struct {
	db           *data.Database
	stmts        *query.Statements
	create       query.Rows[string]
	find         query.Rows[exerciseRow]
	findDue      query.Rows[string]
	lockDue      query.Rows[exerciseRow]
	lockShared   query.Rows[exerciseRow]
	start        query.Statement
	pause        query.Statement
	resume       query.Statement
	stop         query.Statement
	advance      query.Statement
	conclude     query.Statement
	recordRound  query.Statement
	recordOrders query.Statement
	listOrders   query.Rows[ordersRow]
	listRounds   query.Rows[roundRow]
}

// newStore compiles the statements against the service's catalog and binds
// the handles. A compile failure is a wiring defect and panics; no I/O
// happens here.
func newStore(db *data.Database) *store {
	stmts := db.Catalog.MustCompile(files, "statements", db.Dialect())
	return &store{
		db:           db,
		stmts:        stmts,
		create:       stmts.Statement("create").Scan(query.Scalar[string]),
		find:         stmts.Statement("find").Scan(query.Scanner[exerciseRow]()),
		findDue:      stmts.Statement("find_due").Scan(query.Scalar[string]),
		lockDue:      stmts.Statement("lock_due").Scan(query.Scanner[exerciseRow]()),
		lockShared:   stmts.Statement("lock_shared").Scan(query.Scanner[exerciseRow]()),
		start:        stmts.Statement("start"),
		pause:        stmts.Statement("pause"),
		resume:       stmts.Statement("resume"),
		stop:         stmts.Statement("stop"),
		advance:      stmts.Statement("advance"),
		conclude:     stmts.Statement("conclude"),
		recordRound:  stmts.Statement("record_round"),
		recordOrders: stmts.Statement("record_orders"),
		listOrders:   stmts.Statement("list_orders").Scan(query.Scanner[ordersRow]()),
		listRounds:   stmts.Statement("list_rounds").Scan(query.Scanner[roundRow]()),
	}
}

// Verify prepares every statement against the live schema.
func (s *store) Verify(ctx context.Context) error {
	return query.Verify(ctx, s.db, s.stmts)
}

// exerciseRow is an exercise row as the database holds it, its rules
// values still JSON.
type exerciseRow struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Status          string     `json:"status"`
	RoundIntervalMS int64      `json:"round_interval_ms"`
	RoundLimit      int        `json:"round_limit"`
	Round           int        `json:"round"`
	State           []byte     `json:"state"`
	Verdict         []byte     `json:"verdict"`
	NextRoundAt     *time.Time `json:"next_round_at"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// exercise decodes the row into the umpire's view.
func (r exerciseRow) exercise() (Exercise, error) {
	ex := Exercise{
		ID:            r.ID,
		Name:          r.Name,
		Status:        Status(r.Status),
		Round:         r.Round,
		RoundLimit:    r.RoundLimit,
		RoundInterval: (time.Duration(r.RoundIntervalMS) * time.Millisecond).String(),
		NextRoundAt:   r.NextRoundAt,
		CreatedAt:     r.CreatedAt,
		UpdatedAt:     r.UpdatedAt,
		intervalMS:    r.RoundIntervalMS,
	}
	if err := json.Unmarshal(r.State, &ex.State); err != nil {
		return Exercise{}, fmt.Errorf("exercise %s: decode state: %w", r.ID, err)
	}
	ex.Factions = ex.State.Factions
	if r.Verdict != nil {
		var v rules.Verdict
		if err := json.Unmarshal(r.Verdict, &v); err != nil {
			return Exercise{}, fmt.Errorf("exercise %s: decode verdict: %w", r.ID, err)
		}
		ex.Verdict = &v
	}
	return ex, nil
}

// ordersRow is one faction's recorded orders for a round, still JSON.
type ordersRow struct {
	Faction string `json:"faction"`
	Orders  []byte `json:"orders"`
}

// roundRow is one round of the history, its rules values still JSON.
type roundRow struct {
	Round        int       `json:"round"`
	State        []byte    `json:"state"`
	Observations []byte    `json:"observations"`
	Verdict      []byte    `json:"verdict"`
	ResolvedAt   time.Time `json:"resolved_at"`
}

// encode returns v as JSON text, the form a jsonb column is bound from.
func encode(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// validID reports whether id is a UUID, the only form an exercise's id
// takes, so an id that is not one names no exercise rather than failing
// the statement's cast.
func validID(id string) bool {
	_, err := uuid.Parse(id)
	return err == nil
}

// one decodes the single exercise row rows returned, mapping no row to
// ErrNotFound.
func one(id string, r exerciseRow, err error) (Exercise, error) {
	if errors.Is(err, sql.ErrNoRows) {
		return Exercise{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	if err != nil {
		return Exercise{}, fmt.Errorf("read exercise %s: %w", id, err)
	}
	return r.exercise()
}

// insert creates an exercise from a validated command, not yet started,
// and returns its id.
func (s *store) insert(ctx context.Context, tx *sqlate.Tx, c CreateExercise, interval time.Duration) (string, error) {
	state, err := encode(c.state())
	if err != nil {
		return "", fmt.Errorf("encode state: %w", err)
	}
	id, err := s.create.One(ctx, tx, query.Args{
		"name":              c.Name,
		"round_interval_ms": interval.Milliseconds(),
		"round_limit":       c.RoundLimit,
		"state":             state,
	})
	if err != nil {
		return "", fmt.Errorf("insert exercise: %w", err)
	}
	return id, nil
}

// get reads the exercise with the id on sess, the pool or a command's
// transaction, or ErrNotFound. An id that is not a UUID names no exercise.
func (s *store) get(ctx context.Context, sess sqlate.Session, id string) (Exercise, error) {
	if !validID(id) {
		return Exercise{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	r, err := s.find.One(ctx, sess, query.Args{"id": id})
	return one(id, r, err)
}

// due returns the ids of the running exercises whose next round is due,
// without locking them.
func (s *store) due(ctx context.Context) ([]string, error) {
	ids, err := s.findDue.All(ctx, s.db, nil)
	if err != nil {
		return nil, fmt.Errorf("find due exercises: %w", err)
	}
	return ids, nil
}

// lockIfDue locks the exercise for resolving its next round, when it is
// still running, due, and held by no other transaction; ok is false
// otherwise.
func (s *store) lockIfDue(ctx context.Context, tx *sqlate.Tx, id string) (ex Exercise, ok bool, err error) {
	r, err := s.lockDue.One(ctx, tx, query.Args{"id": id})
	if errors.Is(err, sql.ErrNoRows) {
		return Exercise{}, false, nil
	}
	ex, err = one(id, r, err)
	return ex, err == nil, err
}

// share reads the exercise under a shared lock, which waits on a
// resolution in flight, or ErrNotFound.
func (s *store) share(ctx context.Context, tx *sqlate.Tx, id string) (Exercise, error) {
	if !validID(id) {
		return Exercise{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	r, err := s.lockShared.One(ctx, tx, query.Args{"id": id})
	return one(id, r, err)
}

// transition runs one of the status statements and reports whether it
// changed the row: false when the exercise was no longer in the status the
// statement moves it from.
func (s *store) transition(ctx context.Context, tx *sqlate.Tx, st query.Statement, args query.Args) (bool, error) {
	n, err := st.Exec(ctx, tx, args)
	if err != nil {
		return false, fmt.Errorf("%s exercise %v: %w", st.Name(), args["id"], err)
	}
	return n == 1, nil
}

// markStarted moves a created exercise to running, its first round due one
// interval from now.
func (s *store) markStarted(ctx context.Context, tx *sqlate.Tx, id string) (bool, error) {
	return s.transition(ctx, tx, s.start, query.Args{"id": id})
}

// markPaused moves a running exercise to paused.
func (s *store) markPaused(ctx context.Context, tx *sqlate.Tx, id string) (bool, error) {
	return s.transition(ctx, tx, s.pause, query.Args{"id": id})
}

// markResumed moves a paused exercise to running, its next round due one
// interval from now.
func (s *store) markResumed(ctx context.Context, tx *sqlate.Tx, id string) (bool, error) {
	return s.transition(ctx, tx, s.resume, query.Args{"id": id})
}

// markStopped moves the exercise from status from to stopped, with v.
func (s *store) markStopped(ctx context.Context, tx *sqlate.Tx, id string, from Status, v rules.Verdict) (bool, error) {
	verdict, err := encode(v)
	if err != nil {
		return false, fmt.Errorf("encode verdict: %w", err)
	}
	return s.transition(ctx, tx, s.stop, query.Args{"id": id, "from": string(from), "verdict": verdict})
}

// markAdvanced records round and the state it left on a running exercise,
// its next round due one interval from now.
func (s *store) markAdvanced(ctx context.Context, tx *sqlate.Tx, id string, round int, state rules.State) (bool, error) {
	st, err := encode(state)
	if err != nil {
		return false, fmt.Errorf("encode state: %w", err)
	}
	return s.transition(ctx, tx, s.advance, query.Args{"id": id, "round": round, "state": st})
}

// markConcluded moves the exercise from status from to concluded after
// round, with the state it left and its verdict.
func (s *store) markConcluded(ctx context.Context, tx *sqlate.Tx, id string, from Status, round int, state rules.State, v rules.Verdict) (bool, error) {
	st, err := encode(state)
	if err != nil {
		return false, fmt.Errorf("encode state: %w", err)
	}
	verdict, err := encode(v)
	if err != nil {
		return false, fmt.Errorf("encode verdict: %w", err)
	}
	return s.transition(ctx, tx, s.conclude, query.Args{
		"id": id, "from": string(from), "round": round, "state": st, "verdict": verdict,
	})
}

// addRound records one round in the exercise's history.
func (s *store) addRound(ctx context.Context, tx *sqlate.Tx, id string, r Round) error {
	state, err := encode(r.State)
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	obs, err := encode(r.Observations)
	if err != nil {
		return fmt.Errorf("encode observations: %w", err)
	}
	verdict, err := encode(r.Verdict)
	if err != nil {
		return fmt.Errorf("encode verdict: %w", err)
	}
	if _, err := s.recordRound.Exec(ctx, tx, query.Args{
		"exercise_id": id, "round": r.Round, "state": state, "observations": obs, "verdict": verdict,
	}); err != nil {
		return fmt.Errorf("record round %d of exercise %s: %w", r.Round, id, err)
	}
	return nil
}

// putOrders records a faction's orders for a round, replacing any it
// recorded before.
func (s *store) putOrders(ctx context.Context, tx *sqlate.Tx, c RecordOrders) error {
	orders := c.Orders
	if orders == nil {
		orders = []rules.Order{}
	}
	enc, err := encode(orders)
	if err != nil {
		return fmt.Errorf("encode orders: %w", err)
	}
	if _, err := s.recordOrders.Exec(ctx, tx, query.Args{
		"exercise_id": c.Exercise, "faction": c.Faction, "round": c.Round, "orders": enc,
	}); err != nil {
		return fmt.Errorf("record orders of %s for round %d of exercise %s: %w", c.Faction, c.Round, c.Exercise, err)
	}
	return nil
}

// orders returns every faction's recorded orders for a round, keyed by
// faction.
func (s *store) orders(ctx context.Context, tx *sqlate.Tx, id string, round int) (map[string][]rules.Order, error) {
	rows, err := s.listOrders.All(ctx, tx, query.Args{"exercise_id": id, "round": round})
	if err != nil {
		return nil, fmt.Errorf("list orders for round %d of exercise %s: %w", round, id, err)
	}
	out := make(map[string][]rules.Order, len(rows))
	for _, r := range rows {
		var list []rules.Order
		if err := json.Unmarshal(r.Orders, &list); err != nil {
			return nil, fmt.Errorf("decode orders of %s for round %d of exercise %s: %w", r.Faction, round, id, err)
		}
		out[r.Faction] = list
	}
	return out, nil
}

// rounds returns the exercise's round history, round 0 first.
func (s *store) rounds(ctx context.Context, id string) ([]Round, error) {
	rows, err := s.listRounds.All(ctx, s.db, query.Args{"exercise_id": id})
	if err != nil {
		return nil, fmt.Errorf("list rounds of exercise %s: %w", id, err)
	}
	out := make([]Round, len(rows))
	for i, r := range rows {
		out[i] = Round{Round: r.Round, ResolvedAt: r.ResolvedAt}
		if err := errors.Join(
			json.Unmarshal(r.State, &out[i].State),
			json.Unmarshal(r.Observations, &out[i].Observations),
			json.Unmarshal(r.Verdict, &out[i].Verdict),
		); err != nil {
			return nil, fmt.Errorf("decode round %d of exercise %s: %w", r.Round, id, err)
		}
	}
	return out, nil
}
