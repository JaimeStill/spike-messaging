--| tier: standard
--| transaction: required
-- Stops the exercise with the stop's verdict, a rules.Verdict as JSON, when
-- it is still in the status the command read. No row changes when the
-- status moved on meanwhile.
UPDATE exercise
SET status = 'stopped', verdict = {{verdict}}, next_round_at = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE id = {{id:uuid}} AND status = {{from}}
