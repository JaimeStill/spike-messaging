package outbox

import (
	"context"
	"errors"
	"fmt"

	"github.com/JaimeStill/spike-messaging/core/event"
)

const claimInbox = `INSERT INTO messaging_inbox (consumer, source, id) VALUES ($1, $2, $3)
ON CONFLICT DO NOTHING`

// Claim records that consumer is handling e, in the handler's transaction
// tx, and reports whether this is the first time. A handler that gets false
// has already committed its handling of e, so it skips the event and
// acknowledges it. A handler that gets true does its work on the same tx;
// a rollback releases the claim, so a redelivery can claim e again.
//
// consumer names the handler, typically its subscription's Name: consumers
// claim an event independently.
func Claim(ctx context.Context, tx event.Tx, consumer string, e event.Event) (first bool, err error) {
	if consumer == "" {
		return false, errors.New("outbox: claim: consumer is required")
	}
	res, err := tx.ExecContext(ctx, claimInbox, consumer, e.Source, e.ID)
	if err != nil {
		return false, fmt.Errorf("outbox: claim %s for %s: %w", e.ID, consumer, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("outbox: claim %s for %s: %w", e.ID, consumer, err)
	}
	return n == 1, nil
}
