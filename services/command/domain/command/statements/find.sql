--| tier: standard
-- Returns every faction's direction in an exercise, by faction.
SELECT exercise_id, faction, status, map, round, revision, sequence, decisions, closed_round, updated_at
FROM direction
WHERE exercise_id = {{exercise_id:uuid}}
ORDER BY faction
