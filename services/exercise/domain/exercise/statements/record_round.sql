--| tier: standard
--| transaction: required
-- Records one round in the exercise's history: the state after it, both
-- factions' observations, and the verdict, each as JSON, in the
-- transaction that resolved the round.
INSERT INTO exercise_round (exercise_id, round, state, observations, verdict)
VALUES ({{exercise_id:uuid}}, {{round:int}}, {{state}}, {{observations}}, {{verdict}})
