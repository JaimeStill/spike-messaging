-- The rule each element's directive set beside its target, such as secure
-- or retreat, which tells an engaged element's plan whether to withdraw it
-- from its fight. rules maps an element's ID to its rule, and holds the
-- same elements as targets.
ALTER TABLE operation ADD COLUMN rules jsonb NOT NULL DEFAULT '{}';
