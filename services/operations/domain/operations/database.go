package operations

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/query"

	"github.com/JaimeStill/spike-messaging/services/operations/data"
	"github.com/JaimeStill/spike-messaging/services/operations/domain/operations/route"
)

//go:embed statements/*.sql
var files embed.FS

// store is the domain's SQL client. It binds each statement in statements/
// once to a typed handle and exposes them as methods. It is the only file
// that imports the query library and the only place that encodes route's
// values to JSON for their jsonb columns and decodes them back. Every
// method that writes takes the command's transaction.
type store struct {
	db      *data.Database
	stmts   *query.Statements
	open    query.Statement
	lockRow query.Rows[operationRow]
	find    query.Rows[operationRow]
	record  query.Statement
	close   query.Statement
}

// newStore compiles the statements against the service's catalog and binds
// the handles. It performs no I/O. A compile failure is a wiring defect, so
// newStore panics.
func newStore(db *data.Database) *store {
	stmts := db.Catalog.MustCompile(files, "statements", db.Dialect())
	return &store{
		db:      db,
		stmts:   stmts,
		open:    stmts.Statement("open"),
		lockRow: stmts.Statement("lock").Scan(query.Scanner[operationRow]()),
		find:    stmts.Statement("find").Scan(query.Scanner[operationRow]()),
		record:  stmts.Statement("record"),
		close:   stmts.Statement("close"),
	}
}

// Verify prepares every statement against the live schema.
func (s *store) Verify(ctx context.Context) error {
	return query.Verify(ctx, s.db, s.stmts)
}

// operationRow is an operation row as the database holds it, with route's
// values still encoded as JSON.
type operationRow struct {
	ExerciseID string    `json:"exercise_id"`
	Faction    string    `json:"faction"`
	Status     string    `json:"status"`
	Map        []byte    `json:"map"`
	RoundLimit int       `json:"round_limit"`
	LastRound  int       `json:"last_round"`
	Elements   []byte    `json:"elements"`
	Targets    []byte    `json:"targets"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// operation decodes the row.
func (r operationRow) operation() (Operation, error) {
	op := Operation{
		Exercise:  r.ExerciseID,
		Faction:   r.Faction,
		Status:    Status(r.Status),
		LastRound: r.LastRound,
		UpdatedAt: r.UpdatedAt,
		limit:     r.RoundLimit,
	}
	if err := errors.Join(
		json.Unmarshal(r.Map, &op.plan),
		json.Unmarshal(r.Elements, &op.Elements),
		json.Unmarshal(r.Targets, &op.Targets),
	); err != nil {
		return Operation{}, fmt.Errorf("operation %s/%s: decode: %w", r.ExerciseID, r.Faction, err)
	}
	return op, nil
}

// encode returns v as JSON text, the form a jsonb column's parameter is
// bound from.
func encode(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// insert opens one faction's operation over m, with the exercise's round
// limit, unless it is open already.
func (s *store) insert(ctx context.Context, tx *sqlate.Tx, id, faction string, m route.Map, limit int) error {
	enc, err := encode(m)
	if err != nil {
		return fmt.Errorf("encode map: %w", err)
	}
	if _, err := s.open.Exec(ctx, tx, query.Args{"exercise_id": id, "faction": faction, "map": enc, "round_limit": limit}); err != nil {
		return fmt.Errorf("open operation %s/%s: %w", id, faction, err)
	}
	return nil
}

// lock reads one faction's operation under a row lock held for the rest of
// the transaction, or returns [ErrNotOpen] when there is none yet.
func (s *store) lock(ctx context.Context, tx *sqlate.Tx, id, faction string) (Operation, error) {
	r, err := s.lockRow.One(ctx, tx, query.Args{"exercise_id": id, "faction": faction})
	if errors.Is(err, sql.ErrNoRows) {
		return Operation{}, fmt.Errorf("%w: %s/%s", ErrNotOpen, id, faction)
	}
	if err != nil {
		return Operation{}, fmt.Errorf("lock operation %s/%s: %w", id, faction, err)
	}
	return r.operation()
}

// all returns every faction's operation in exercise id, by faction.
func (s *store) all(ctx context.Context, id string) ([]Operation, error) {
	rows, err := s.find.All(ctx, s.db, query.Args{"exercise_id": id})
	if err != nil {
		return nil, fmt.Errorf("find operations of %s: %w", id, err)
	}
	out := make([]Operation, len(rows))
	for i, r := range rows {
		if out[i], err = r.operation(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// save records op's elements, targets, and last round.
func (s *store) save(ctx context.Context, tx *sqlate.Tx, op Operation) error {
	elements := op.Elements
	if elements == nil {
		elements = []route.Element{}
	}
	targets := op.Targets
	if targets == nil {
		targets = map[string]route.Location{}
	}
	enc, err := encode(elements)
	if err != nil {
		return fmt.Errorf("encode elements: %w", err)
	}
	tgt, err := encode(targets)
	if err != nil {
		return fmt.Errorf("encode targets: %w", err)
	}
	if _, err := s.record.Exec(ctx, tx, query.Args{
		"exercise_id": op.Exercise, "faction": op.Faction,
		"elements": enc, "targets": tgt, "last_round": op.LastRound,
	}); err != nil {
		return fmt.Errorf("save operation %s/%s: %w", op.Exercise, op.Faction, err)
	}
	return nil
}

// closeAll closes every faction's operation in exercise id and reports how
// many it found.
func (s *store) closeAll(ctx context.Context, tx *sqlate.Tx, id string) (int64, error) {
	n, err := s.close.Exec(ctx, tx, query.Args{"exercise_id": id})
	if err != nil {
		return 0, fmt.Errorf("close operations of %s: %w", id, err)
	}
	return n, nil
}
