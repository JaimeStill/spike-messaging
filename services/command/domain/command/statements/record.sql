--| tier: standard
--| transaction: required
-- Records the round and the revision of the assessment one faction decided
-- on, the sequence of the last directive it issued, and its decisions.
UPDATE direction
SET round = {{round:int}}, revision = {{revision:int}}, sequence = {{sequence:int}},
    decisions = {{decisions}}, updated_at = CURRENT_TIMESTAMP
WHERE exercise_id = {{exercise_id:uuid}} AND faction = {{faction}}
