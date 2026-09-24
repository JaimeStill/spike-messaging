--| tier: standard
-- Counts the outbox rows the relay has not yet published: the outbox's lag,
-- for a consumer that reports or monitors it. The relay never runs it.
SELECT count(*) FROM messaging_outbox WHERE published_at IS NULL
