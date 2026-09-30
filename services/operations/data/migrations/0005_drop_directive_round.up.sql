-- The round of the last applied directive guarded nothing once the
-- directive's sequence did, so it goes.
ALTER TABLE operation DROP COLUMN IF EXISTS directive_round;
