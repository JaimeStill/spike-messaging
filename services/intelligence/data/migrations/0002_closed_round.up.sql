-- The round an assessment's exercise concluded after, set when it closes.
-- The final round's observations and the conclusion are raised together
-- and consumed apart, so a closed assessment still takes an observation up
-- to this round.
ALTER TABLE assessment ADD COLUMN closed_round int
