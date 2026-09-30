--| tier: native
--| transaction: required
--| native: RETURNING, so the insert returns the id the uuidv7() default mints. Port: OUTPUT INSERTED (SQL Server), RETURNING INTO (Oracle), an id minted by the program (MySQL, SQLite).
-- Creates an exercise, not yet started: round 0, its seed, and the starting
-- state, which Create validated. state is a rules.State as JSON, typed by
-- its column, so the statement needs no cast.
INSERT INTO exercise (name, seed, status, round_interval_ms, round_limit, state)
VALUES ({{name}}, {{seed:bigint}}, 'created', {{round_interval_ms:bigint}}, {{round_limit:int}}, {{state}})
RETURNING id
