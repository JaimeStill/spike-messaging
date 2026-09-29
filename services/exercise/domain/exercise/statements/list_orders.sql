--| tier: standard
--| transaction: required
-- Returns both factions' orders recorded for a round. It runs in the
-- resolution's transaction, which holds the exercise's lock, so no order
-- for the round is recorded while the round resolves.
SELECT faction, orders
FROM exercise_orders
WHERE exercise_id = {{exercise_id:uuid}} AND round = {{round:int}}
ORDER BY faction
