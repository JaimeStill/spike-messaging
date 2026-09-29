--| tier: native
--| transaction: required
--| native: interval arithmetic, a bigint of milliseconds times INTERVAL '1 millisecond' added to CURRENT_TIMESTAMP. Port: DATE_ADD with MICROSECOND (MySQL), DATEADD(millisecond, ...) (SQL Server), NUMTODSINTERVAL (Oracle), datetime with a modifier (SQLite).
-- Starts a created exercise: its first round is due one interval from the
-- database's clock. No row changes when the exercise is no longer created.
UPDATE exercise
SET status = 'running', round = 0,
    next_round_at = CURRENT_TIMESTAMP + round_interval_ms * INTERVAL '1 millisecond',
    updated_at = CURRENT_TIMESTAMP
WHERE id = {{id:uuid}} AND status = 'created'
