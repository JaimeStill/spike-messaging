UPDATE assessment SET picture = picture || jsonb_build_object('grid', grid);

ALTER TABLE assessment DROP COLUMN IF EXISTS grid;
