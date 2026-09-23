package event

import (
	"context"
	"database/sql"
)

// Tx is the transaction an event is written in: the one that makes the state
// the event reports true. sqlate's *Tx satisfies it. So does a pool, which
// the type cannot prevent; passing one breaks the emission rule.
type Tx interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// Emitter is the one messaging dependency a domain takes. The composition
// root injects the implementation, an outbox, so the domain never sees a
// broker.
type Emitter interface {
	Emit(ctx context.Context, tx Tx, e Event) error
}
