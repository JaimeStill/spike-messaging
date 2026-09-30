package command

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

	"github.com/JaimeStill/spike-messaging/services/command/data"
	"github.com/JaimeStill/spike-messaging/services/command/domain/command/decide"
)

//go:embed statements/*.sql
var files embed.FS

// store is the domain's SQL client. It binds each statement in statements/
// once to a typed handle and exposes them as methods. It is the only file
// that imports the query library and the only place that encodes decide's
// map and decisions to JSON for their jsonb columns and decodes them back.
// Every method that writes takes the command's transaction.
type store struct {
	db      *data.Database
	stmts   *query.Statements
	open    query.Statement
	lockRow query.Rows[directionRow]
	find    query.Rows[directionRow]
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
		lockRow: stmts.Statement("lock").Scan(query.Scanner[directionRow]()),
		find:    stmts.Statement("find").Scan(query.Scanner[directionRow]()),
		record:  stmts.Statement("record"),
		close:   stmts.Statement("close"),
	}
}

// Verify prepares every statement against the live schema.
func (s *store) Verify(ctx context.Context) error {
	return query.Verify(ctx, s.db, s.stmts)
}

// directionRow is a direction row as the database holds it, with its map
// and decisions still encoded as JSON.
type directionRow struct {
	ExerciseID  string    `json:"exercise_id"`
	Faction     string    `json:"faction"`
	Status      string    `json:"status"`
	Map         []byte    `json:"map"`
	Round       int       `json:"round"`
	Decisions   []byte    `json:"decisions"`
	ClosedRound *int      `json:"closed_round"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// direction decodes the row.
func (r directionRow) direction() (Direction, error) {
	d := Direction{
		Exercise:  r.ExerciseID,
		Faction:   r.Faction,
		Status:    Status(r.Status),
		Round:     r.Round,
		UpdatedAt: r.UpdatedAt,
	}
	if r.ClosedRound != nil {
		d.closedRound = *r.ClosedRound
	}
	if err := json.Unmarshal(r.Map, &d.plan); err != nil {
		return Direction{}, fmt.Errorf("direction %s/%s: decode map: %w", r.ExerciseID, r.Faction, err)
	}
	if err := json.Unmarshal(r.Decisions, &d.Decisions); err != nil {
		return Direction{}, fmt.Errorf("direction %s/%s: decode decisions: %w", r.ExerciseID, r.Faction, err)
	}
	return d, nil
}

// encode returns v as JSON text, the form a jsonb column's parameter is
// bound from.
func encode(what string, v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("encode %s: %w", what, err)
	}
	return string(b), nil
}

// insert opens one faction's direction over map m, unless it is open
// already.
func (s *store) insert(ctx context.Context, tx *sqlate.Tx, id, faction string, m decide.Map) error {
	enc, err := encode("map", m)
	if err != nil {
		return err
	}
	if _, err := s.open.Exec(ctx, tx, query.Args{"exercise_id": id, "faction": faction, "map": enc}); err != nil {
		return fmt.Errorf("open direction %s/%s: %w", id, faction, err)
	}
	return nil
}

// lock reads one faction's direction under a row lock held for the rest of
// the transaction, or returns [ErrNotOpen] when there is none yet.
func (s *store) lock(ctx context.Context, tx *sqlate.Tx, id, faction string) (Direction, error) {
	r, err := s.lockRow.One(ctx, tx, query.Args{"exercise_id": id, "faction": faction})
	if errors.Is(err, sql.ErrNoRows) {
		return Direction{}, fmt.Errorf("%w: %s/%s", ErrNotOpen, id, faction)
	}
	if err != nil {
		return Direction{}, fmt.Errorf("lock direction %s/%s: %w", id, faction, err)
	}
	return r.direction()
}

// all returns every faction's direction in exercise id, by faction.
func (s *store) all(ctx context.Context, id string) ([]Direction, error) {
	rows, err := s.find.All(ctx, s.db, query.Args{"exercise_id": id})
	if err != nil {
		return nil, fmt.Errorf("find directions of %s: %w", id, err)
	}
	out := make([]Direction, len(rows))
	for i, r := range rows {
		if out[i], err = r.direction(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// save records d's round and decisions.
func (s *store) save(ctx context.Context, tx *sqlate.Tx, d Direction) error {
	decisions := d.Decisions
	if decisions == nil {
		decisions = []decide.Decision{}
	}
	enc, err := encode("decisions", decisions)
	if err != nil {
		return err
	}
	if _, err := s.record.Exec(ctx, tx, query.Args{
		"exercise_id": d.Exercise, "faction": d.Faction, "round": d.Round, "decisions": enc,
	}); err != nil {
		return fmt.Errorf("save direction %s/%s: %w", d.Exercise, d.Faction, err)
	}
	return nil
}

// closeAll closes every faction's direction in exercise id, which
// concluded after round, and reports how many directions it closed.
func (s *store) closeAll(ctx context.Context, tx *sqlate.Tx, id string, round int) (int64, error) {
	n, err := s.close.Exec(ctx, tx, query.Args{"exercise_id": id, "closed_round": round})
	if err != nil {
		return 0, fmt.Errorf("close directions of %s: %w", id, err)
	}
	return n, nil
}
