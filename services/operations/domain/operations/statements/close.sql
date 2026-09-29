--| tier: standard
--| transaction: required
-- Closes every faction's operation in an exercise.
UPDATE operation
SET status = 'closed', updated_at = CURRENT_TIMESTAMP
WHERE exercise_id = {{exercise_id:uuid}}
