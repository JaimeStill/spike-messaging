package intelligence

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

	"github.com/JaimeStill/spike-messaging/services/intelligence/data"
	"github.com/JaimeStill/spike-messaging/services/intelligence/domain/intelligence/fusion"
)

//go:embed statements/*.sql
var files embed.FS

// store is the domain's SQL client. It binds each statement in statements/
// once to a typed handle and exposes them as methods. It is the only file
// that imports the query library and the only place that encodes fusion's
// picture to JSON for its jsonb column and decodes it back. Every method
// that writes takes the command's transaction.
type store struct {
	db      *data.Database
	stmts   *query.Statements
	open    query.Statement
	lockRow query.Rows[assessmentRow]
	find    query.Rows[assessmentRow]
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
		lockRow: stmts.Statement("lock").Scan(query.Scanner[assessmentRow]()),
		find:    stmts.Statement("find").Scan(query.Scanner[assessmentRow]()),
		record:  stmts.Statement("record"),
		close:   stmts.Statement("close"),
	}
}

// Verify prepares every statement against the live schema.
func (s *store) Verify(ctx context.Context) error {
	return query.Verify(ctx, s.db, s.stmts)
}

// assessmentRow is an assessment row as the database holds it, with its
// picture still encoded as JSON.
type assessmentRow struct {
	ExerciseID string    `json:"exercise_id"`
	Faction    string    `json:"faction"`
	Status     string    `json:"status"`
	Picture    []byte    `json:"picture"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// assessment decodes the row.
func (r assessmentRow) assessment() (Assessment, error) {
	a := Assessment{
		Exercise:  r.ExerciseID,
		Faction:   r.Faction,
		Status:    Status(r.Status),
		UpdatedAt: r.UpdatedAt,
	}
	if err := json.Unmarshal(r.Picture, &a.Picture); err != nil {
		return Assessment{}, fmt.Errorf("assessment %s/%s: decode: %w", r.ExerciseID, r.Faction, err)
	}
	return a, nil
}

// encode returns p as JSON text, the form a jsonb column's parameter is
// bound from.
func encode(p fusion.Picture) (string, error) {
	b, err := json.Marshal(p)
	if err != nil {
		return "", fmt.Errorf("encode picture: %w", err)
	}
	return string(b), nil
}

// insert opens one faction's assessment from picture p, unless it is open
// already.
func (s *store) insert(ctx context.Context, tx *sqlate.Tx, id, faction string, p fusion.Picture) error {
	enc, err := encode(p)
	if err != nil {
		return err
	}
	if _, err := s.open.Exec(ctx, tx, query.Args{"exercise_id": id, "faction": faction, "picture": enc}); err != nil {
		return fmt.Errorf("open assessment %s/%s: %w", id, faction, err)
	}
	return nil
}

// lock reads one faction's assessment under a row lock held for the rest of
// the transaction, or returns [ErrNotOpen] when there is none yet.
func (s *store) lock(ctx context.Context, tx *sqlate.Tx, id, faction string) (Assessment, error) {
	r, err := s.lockRow.One(ctx, tx, query.Args{"exercise_id": id, "faction": faction})
	if errors.Is(err, sql.ErrNoRows) {
		return Assessment{}, fmt.Errorf("%w: %s/%s", ErrNotOpen, id, faction)
	}
	if err != nil {
		return Assessment{}, fmt.Errorf("lock assessment %s/%s: %w", id, faction, err)
	}
	return r.assessment()
}

// all returns every faction's assessment in exercise id, by faction.
func (s *store) all(ctx context.Context, id string) ([]Assessment, error) {
	rows, err := s.find.All(ctx, s.db, query.Args{"exercise_id": id})
	if err != nil {
		return nil, fmt.Errorf("find assessments of %s: %w", id, err)
	}
	out := make([]Assessment, len(rows))
	for i, r := range rows {
		if out[i], err = r.assessment(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// save records a's picture.
func (s *store) save(ctx context.Context, tx *sqlate.Tx, a Assessment) error {
	enc, err := encode(a.Picture)
	if err != nil {
		return err
	}
	if _, err := s.record.Exec(ctx, tx, query.Args{
		"exercise_id": a.Exercise, "faction": a.Faction, "picture": enc,
	}); err != nil {
		return fmt.Errorf("save assessment %s/%s: %w", a.Exercise, a.Faction, err)
	}
	return nil
}

// closeAll closes every faction's assessment in exercise id and reports how
// many it found.
func (s *store) closeAll(ctx context.Context, tx *sqlate.Tx, id string) (int64, error) {
	n, err := s.close.Exec(ctx, tx, query.Args{"exercise_id": id})
	if err != nil {
		return 0, fmt.Errorf("close assessments of %s: %w", id, err)
	}
	return n, nil
}
