--| tier: native
--| transaction: required
--| native: FOR SHARE, so recording an order waits on a resolution in flight and reads the round it left. Port: LOCK IN SHARE MODE or FOR SHARE (MySQL), HOLDLOCK (SQL Server); Oracle has no shared row lock, so FOR UPDATE serves.
-- Returns the exercise, share-locked so an order can be recorded against
-- its current round, or no row.
SELECT id, name, seed, status, round_interval_ms, round_limit, round, state, verdict,
       next_round_at, created_at, updated_at
FROM exercise
WHERE id = {{id:uuid}}
FOR SHARE
