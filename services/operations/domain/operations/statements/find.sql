--| tier: standard
-- Returns every faction's operation in an exercise, by faction.
SELECT exercise_id, faction, status, map, round_limit, last_round, directive_round, directive_sequence, elements, targets, rules, updated_at
FROM operation
WHERE exercise_id = {{exercise_id:uuid}}
ORDER BY faction
