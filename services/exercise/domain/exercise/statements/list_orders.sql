--| tier: standard
--| transaction: required
-- Both factions' orders recorded for a round, in the resolution's
-- transaction, which holds the exercise's lock, so no order for the round
-- is recorded while the round resolves.
SELECT faction, orders
FROM exercise_orders
WHERE exercise_id = {{exercise_id:uuid}} AND round = {{round:int}}
ORDER BY faction
