--| tier: standard
-- The running exercises whose next round is due by the database's clock,
-- the earliest first. It locks nothing: the resolver locks each one in its
-- own transaction (lock_due) and skips one that has changed meanwhile.
SELECT id
FROM exercise
WHERE status = 'running' AND next_round_at <= CURRENT_TIMESTAMP
ORDER BY next_round_at, id
