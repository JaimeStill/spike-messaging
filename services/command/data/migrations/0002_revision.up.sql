-- The revision of the last assessment the direction decided on, 0 before
-- the first, so an assessment decided out of order, one of a revision no
-- higher, is skipped; and the sequence of the last directive it issued, 0
-- before the first, which each directive carries so a consumer can skip one
-- that arrives out of order.
ALTER TABLE direction
    ADD COLUMN revision int NOT NULL DEFAULT 0,
    ADD COLUMN sequence int NOT NULL DEFAULT 0
