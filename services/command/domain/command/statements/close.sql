--| tier: standard
--| transaction: required
-- Closes every faction's direction in an exercise, recording the round it
-- concluded after.
UPDATE direction
SET status = 'closed', closed_round = {{closed_round:int}}, updated_at = CURRENT_TIMESTAMP
WHERE exercise_id = {{exercise_id:uuid}}
