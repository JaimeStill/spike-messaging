--| tier: standard
-- Returns the exercise's round history, round 0 first.
SELECT round, state, observations, verdict, resolved_at
FROM exercise_round
WHERE exercise_id = {{exercise_id:uuid}}
ORDER BY round
