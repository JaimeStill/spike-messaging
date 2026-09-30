--| tier: native
--| transaction: required
--| native: INSERT ... ON CONFLICT DO NOTHING, so a second start event for an exercise opens nothing new. Port: MERGE (SQL Server, Oracle), INSERT IGNORE (MySQL), ON CONFLICT DO NOTHING (SQLite).
-- Opens one faction's assessment in an exercise from the picture it starts
-- from, a fusion.Picture as JSON, and the map's grid, unless it is already
-- open.
INSERT INTO assessment (exercise_id, faction, picture, grid)
VALUES ({{exercise_id:uuid}}, {{faction}}, {{picture}}, {{grid}})
ON CONFLICT (exercise_id, faction) DO NOTHING
