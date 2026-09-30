--| tier: standard
--| transaction: required
-- Records one round in the exercise's history: the state after it, both
-- factions' observations, the verdict, and the round's resolution, each as
-- JSON, in the transaction that resolved the round. The start, round 0,
-- resolves nothing, so its resolution is null.
INSERT INTO exercise_round (exercise_id, round, state, observations, verdict, resolution)
VALUES ({{exercise_id:uuid}}, {{round:int}}, {{state}}, {{observations}}, {{verdict}}, {{resolution}})
