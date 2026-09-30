-- The round of the last directive an operation applied, -1 before the
-- first. Each directive carries its faction's whole target state, so the
-- newest applies whenever it arrives, even behind the operation's last
-- observed round, and only an older one is stale.
ALTER TABLE operation ADD COLUMN directive_round int NOT NULL DEFAULT -1;
