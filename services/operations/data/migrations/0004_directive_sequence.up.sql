-- The sequence of the last directive an operation applied, 0 before the
-- first. Command numbers each faction's directives in the order it decides
-- them, so a directive whose sequence is no higher than this one is stale,
-- whichever round it names.
ALTER TABLE operation ADD COLUMN directive_sequence int NOT NULL DEFAULT 0;
