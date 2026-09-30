-- The grid holds the map's sectors, the fusion.Sector list that bounds the
-- picture's explored cells. The picture's JSON leaves the grid out, so the
-- grid has a column of its own. An assessment opened before the column
-- existed kept the grid in its picture; this migration moves it.
ALTER TABLE assessment ADD COLUMN grid jsonb NOT NULL DEFAULT '[]';

UPDATE assessment SET grid = picture -> 'grid', picture = picture - 'grid' WHERE picture ? 'grid';

ALTER TABLE assessment ALTER COLUMN grid DROP DEFAULT;
