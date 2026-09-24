--| tier: native
--| native: FOR UPDATE SKIP LOCKED with LIMIT, so relays share the outbox without waiting on each other's rows. Port: an engine must lock one row for the transaction and skip rows other transactions hold (MySQL 8 and Oracle have SKIP LOCKED; SQL Server has READPAST with UPDLOCK); without it, one relay runs at a time.
--| transaction: required
-- Claims the oldest unpublished row no other relay holds. The lock lasts
-- until the relay's transaction marks the row published and commits, or
-- rolls back and leaves it for the next pass.
SELECT seq, header, data
FROM messaging_outbox
WHERE published_at IS NULL
ORDER BY seq
LIMIT 1
FOR UPDATE SKIP LOCKED
