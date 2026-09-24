package event

import (
	"context"
	"database/sql"
)

// Tx is the transaction an event is written in: the one that makes the state
// the event reports true. sqlate's *Tx and database/sql's *Tx satisfy it. A
// pool does not: Commit and Rollback are in the method set so that writing an
// event outside a transaction fails to compile. The rest of the method set is
// a database/sql session's, so an emitter can hand a Tx to a query runner
// that takes one, such as sqlate's. An emitter only runs statements on a Tx;
// the code that began the transaction ends it.
type Tx interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	PrepareContext(ctx context.Context, query string) (*sql.Stmt, error)
	Commit() error
	Rollback() error
}

// Emitter is the one messaging dependency a domain takes. The composition
// root injects the implementation, an outbox, so the domain never sees a
// broker.
type Emitter interface {
	Emit(ctx context.Context, tx Tx, e Event) error
}
