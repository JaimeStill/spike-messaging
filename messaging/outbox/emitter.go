package outbox

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/standards-lab/sqlate/query"

	"github.com/JaimeStill/spike-messaging/core/event"
)

// Emitter writes each event as an outbox row in the caller's transaction.
// The event becomes visible to the relay when that transaction commits, and
// is discarded with it on a rollback.
type Emitter struct{}

var _ event.Emitter = Emitter{}

// NewEmitter returns the outbox's emitter.
func NewEmitter() Emitter { return Emitter{} }

// Emit validates and encodes e and inserts it on tx. An event whose source
// and id are already in the outbox fails, and so fails the transaction.
func (Emitter) Emit(ctx context.Context, tx event.Tx, e event.Event) error {
	h, data, err := event.Encode(e)
	if err != nil {
		return fmt.Errorf("outbox: emit: %w", err)
	}
	header, err := json.Marshal(h)
	if err != nil {
		return fmt.Errorf("outbox: emit %s: %w", e.ID, err)
	}
	args := query.Args{"source": e.Source, "id": e.ID, "header": header, "data": data}
	if _, err := emitStmt.Exec(ctx, tx, args); err != nil {
		return fmt.Errorf("outbox: emit %s: %w", e.ID, err)
	}
	return nil
}
