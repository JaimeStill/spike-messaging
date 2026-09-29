--| tier: native
--| transaction: required
--| native: INSERT ... ON CONFLICT DO NOTHING, so a second start event for an exercise opens nothing new. Port: MERGE (SQL Server, Oracle), INSERT IGNORE (MySQL), ON CONFLICT DO NOTHING (SQLite).
-- Opens one faction's operation in an exercise over the exercise's public
-- map, a route.Map as JSON, and its round limit, unless it is already open.
INSERT INTO operation (exercise_id, faction, map, round_limit)
VALUES ({{exercise_id:uuid}}, {{faction}}, {{map}}, {{round_limit:int}})
ON CONFLICT (exercise_id, faction) DO NOTHING
