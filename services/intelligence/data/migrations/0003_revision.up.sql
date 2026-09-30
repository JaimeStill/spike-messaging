-- How many assessments have been issued from this row: each
-- intelligence.assessment.issued carries the count after it, so a consumer
-- can tell a later assessment of a round from an earlier one.
ALTER TABLE assessment ADD COLUMN revision int NOT NULL DEFAULT 0
