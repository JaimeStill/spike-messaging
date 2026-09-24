--| tier: standard
-- Writes one event into the outbox, in the transaction that makes the state
-- it reports true. The header is event.Encode's headers as JSON, typed by
-- the column it is inserted into, so the statement needs no cast. The
-- transaction requirement is event.Tx's, enforced at compile time, so the
-- file does not declare it: a database/sql *Tx satisfies event.Tx and would
-- be refused by the declaration. A second event with the same source and id
-- is messaging_uq_outbox_event's unique violation, which fails the
-- transaction.
INSERT INTO messaging_outbox (source, id, header, data)
VALUES ({{source}}, {{id}}, {{header}}, {{data}})
