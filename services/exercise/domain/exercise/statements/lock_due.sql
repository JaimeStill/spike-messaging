--| tier: native
--| transaction: required
--| native: FOR UPDATE SKIP LOCKED, so replicas resolving at once never resolve one exercise twice and never wait on each other. Port: an engine must lock the row for the transaction and skip one another transaction holds (MySQL 8 and Oracle have SKIP LOCKED; SQL Server has READPAST with UPDLOCK); without it, one resolver runs at a time.
-- Locks the exercise for the resolution of its next round when it is still
-- running and due, or returns no row: another replica holds it, an order is
-- being recorded for it, or it paused, stopped, or was resolved since the
-- resolver found it due.
SELECT id, name, status, round_interval_ms, round_limit, round, state, verdict,
       next_round_at, created_at, updated_at
FROM exercise
WHERE id = {{id:uuid}} AND status = 'running' AND next_round_at <= CURRENT_TIMESTAMP
FOR UPDATE SKIP LOCKED
