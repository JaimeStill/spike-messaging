--| tier: native
--| transaction: required
--| native: FOR UPDATE, so a status command waits on a resolution in flight and reads the round and status it left. Port: FOR UPDATE (MySQL, Oracle), UPDLOCK with HOLDLOCK (SQL Server).
-- Returns the exercise, locked for the rest of the transaction so a status
-- command decides and raises from its current status and round, or no row.
SELECT id, name, seed, status, round_interval_ms, round_limit, round, state, verdict,
       next_round_at, created_at, updated_at
FROM exercise
WHERE id = {{id:uuid}}
FOR UPDATE
