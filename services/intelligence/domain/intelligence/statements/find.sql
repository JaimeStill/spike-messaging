--| tier: standard
-- Returns every faction's assessment in an exercise, by faction.
SELECT exercise_id, faction, status, picture, updated_at
FROM assessment
WHERE exercise_id = {{exercise_id:uuid}}
ORDER BY faction
