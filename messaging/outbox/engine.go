package outbox

import (
	"errors"
	"fmt"
	"slices"

	"github.com/standards-lab/sqlate/query"
)

// Engine is the SQL a database engine's module supplies: every statement the
// outbox runs, compiled for that engine. The outbox holds no SQL of its own,
// so an engine is required, and it must define each statement with exactly
// the parameters named here. [New] checks both.
type Engine struct {
	// Emit inserts one event into the outbox. Parameters: source, id,
	// header (event.Encode's headers as JSON), and data. An event whose
	// source and id are already in the outbox must fail.
	Emit query.Statement
	// ClaimRow locks the oldest unpublished row not already held by another
	// transaction, skipping held rows, for the rest of the transaction. It
	// takes no parameters and returns seq, header, and data, in that order,
	// or no row.
	ClaimRow query.Statement
	// MarkPublished marks the claimed row published. Parameter: seq.
	MarkPublished query.Statement
	// ClaimInbox records a consumer's handling of an event, affecting one
	// row on the first claim and none on a repeat. Parameters: consumer,
	// source, and id.
	ClaimInbox query.Statement
}

// validate reports every statement the engine leaves undefined or defines
// with the wrong parameters.
func (e Engine) validate() error {
	var errs []error
	check := func(field string, st query.Statement, params ...string) {
		if st.Name() == "" {
			errs = append(errs, fmt.Errorf("%s is not defined", field))
			return
		}
		got := slices.Sorted(slices.Values(st.Params()))
		want := slices.Sorted(slices.Values(params))
		if !slices.Equal(got, want) {
			errs = append(errs, fmt.Errorf("%s (%s) takes parameters %v, want %v", field, st.Name(), got, want))
		}
	}
	check("Emit", e.Emit, "source", "id", "header", "data")
	check("ClaimRow", e.ClaimRow)
	check("MarkPublished", e.MarkPublished, "seq")
	check("ClaimInbox", e.ClaimInbox, "consumer", "source", "id")
	if len(errs) > 0 {
		return fmt.Errorf("outbox: engine: %w", errors.Join(errs...))
	}
	return nil
}
