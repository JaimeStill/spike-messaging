--| tier: standard
--| transaction: required
-- Pauses a running exercise: no round is due until it resumes. No row
-- changes when the exercise is not running.
UPDATE exercise
SET status = 'paused', next_round_at = NULL, updated_at = CURRENT_TIMESTAMP
WHERE id = {{id:uuid}} AND status = 'running'
