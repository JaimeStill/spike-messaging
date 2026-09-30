-- The map's sectors, as the fusion.Sector list the picture keeps its
-- explored cells within. The picture's JSON leaves the grid out, so it has
-- a column of its own; an assessment opened before the column existed
-- held it in the picture, and it moves here.
ALTER TABLE assessment ADD COLUMN grid jsonb NOT NULL DEFAULT '[]';

UPDATE assessment SET grid = picture -> 'grid', picture = picture - 'grid' WHERE picture ? 'grid';

ALTER TABLE assessment ALTER COLUMN grid DROP DEFAULT;
