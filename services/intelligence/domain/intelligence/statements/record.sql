--| tier: standard
--| transaction: required
-- Records one faction's picture and its revision.
UPDATE assessment
SET picture = {{picture}}, revision = {{revision:int}}, updated_at = CURRENT_TIMESTAMP
WHERE exercise_id = {{exercise_id:uuid}} AND faction = {{faction}}
