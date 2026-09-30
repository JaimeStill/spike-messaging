--| tier: standard
--| transaction: required
-- Records one faction's elements, their targets and the rules that set
-- them, the last round acted on, and the sequence of the last applied
-- directive.
UPDATE operation
SET elements = {{elements}}, targets = {{targets}}, rules = {{rules}}, last_round = {{last_round:int}}, directive_sequence = {{directive_sequence:int}}, updated_at = CURRENT_TIMESTAMP
WHERE exercise_id = {{exercise_id:uuid}} AND faction = {{faction}}
