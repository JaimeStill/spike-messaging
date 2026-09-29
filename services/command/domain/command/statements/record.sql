--| tier: standard
--| transaction: required
-- Records the round one faction decided on, and its decisions.
UPDATE direction
SET round = {{round:int}}, decisions = {{decisions}}, updated_at = CURRENT_TIMESTAMP
WHERE exercise_id = {{exercise_id:uuid}} AND faction = {{faction}}
