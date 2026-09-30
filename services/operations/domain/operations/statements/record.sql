--| tier: standard
--| transaction: required
-- Records one faction's elements, targets, last acted-on round, and last
-- applied directive's round.
UPDATE operation
SET elements = {{elements}}, targets = {{targets}}, last_round = {{last_round:int}}, directive_round = {{directive_round:int}}, updated_at = CURRENT_TIMESTAMP
WHERE exercise_id = {{exercise_id:uuid}} AND faction = {{faction}}
