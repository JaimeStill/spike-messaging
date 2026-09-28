--| tier: native
--| transaction: required
--| native: interval arithmetic, a bigint of milliseconds times INTERVAL '1 millisecond' added to CURRENT_TIMESTAMP. Port: DATE_ADD with MICROSECOND (MySQL), DATEADD(millisecond, ...) (SQL Server), NUMTODSINTERVAL (Oracle), datetime with a modifier (SQLite).
-- Records a resolved round that did not end the exercise: its number and
-- the state it left, a rules.State as JSON, with the next round due one
-- interval from the database's clock. The resolver holds the row's lock.
UPDATE exercise
SET round = {{round:int}}, state = {{state}},
    next_round_at = CURRENT_TIMESTAMP + round_interval_ms * INTERVAL '1 millisecond',
    updated_at = CURRENT_TIMESTAMP
WHERE id = {{id:uuid}} AND status = 'running'
