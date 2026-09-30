--| tier: standard
--| transaction: required
-- Records one faction's elements, targets, the rules that set them, last
-- acted-on round, and the round and sequence of the last applied directive.
UPDATE operation
SET elements = {{elements}}, targets = {{targets}}, rules = {{rules}}, last_round = {{last_round:int}}, directive_round = {{directive_round:int}}, directive_sequence = {{directive_sequence:int}}, updated_at = CURRENT_TIMESTAMP
WHERE exercise_id = {{exercise_id:uuid}} AND faction = {{faction}}
