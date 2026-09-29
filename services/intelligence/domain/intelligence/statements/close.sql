--| tier: standard
--| transaction: required
-- Closes every faction's assessment in an exercise.
UPDATE assessment
SET status = 'closed', updated_at = CURRENT_TIMESTAMP
WHERE exercise_id = {{exercise_id:uuid}}
