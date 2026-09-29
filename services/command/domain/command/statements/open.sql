--| tier: native
--| transaction: required
--| native: INSERT ... ON CONFLICT DO NOTHING, so a second start event for an exercise opens nothing new. Port: MERGE (SQL Server, Oracle), INSERT IGNORE (MySQL), ON CONFLICT DO NOTHING (SQLite).
-- Opens one faction's direction in an exercise over the map, a decide.Map as
-- JSON, unless it is already open.
INSERT INTO direction (exercise_id, faction, map)
VALUES ({{exercise_id:uuid}}, {{faction}}, {{map}})
ON CONFLICT (exercise_id, faction) DO NOTHING
