-- directive_round, the round of the last applied directive, guarded nothing
-- once directive_sequence took over that guard, so this migration drops it.
ALTER TABLE operation DROP COLUMN IF EXISTS directive_round;
