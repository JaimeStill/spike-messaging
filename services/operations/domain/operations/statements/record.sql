--| tier: standard
--| transaction: required
-- Records one faction's elements, targets, and last acted-on round.
UPDATE operation
SET elements = {{elements}}, targets = {{targets}}, last_round = {{last_round:int}}, updated_at = CURRENT_TIMESTAMP
WHERE exercise_id = {{exercise_id:uuid}} AND faction = {{faction}}
