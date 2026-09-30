-- The seed every round's random draws come from, so a seed replays an
-- exercise. An exercise created before the column existed takes 0; the
-- default then drops, so every new exercise names its own seed.
ALTER TABLE exercise ADD COLUMN seed bigint NOT NULL DEFAULT 0;

ALTER TABLE exercise ALTER COLUMN seed DROP DEFAULT;

-- The round's resolution, a rules.Resolution as JSON: what the round's
-- retreats, fights, and captures did. It is null for round 0, the start,
-- which resolves nothing, and for a round recorded before the column
-- existed.
ALTER TABLE exercise_round ADD COLUMN resolution jsonb;
