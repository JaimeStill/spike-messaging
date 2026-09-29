--| tier: standard
--| transaction: required
-- Records one faction's picture.
UPDATE assessment
SET picture = {{picture}}, updated_at = CURRENT_TIMESTAMP
WHERE exercise_id = {{exercise_id:uuid}} AND faction = {{faction}}
