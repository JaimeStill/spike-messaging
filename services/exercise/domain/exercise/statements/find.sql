--| tier: standard
-- Returns the exercise with the id, as the umpire sees it, or no row. The
-- SELECT list is in the order the store's row type scans.
SELECT id, name, seed, status, round_interval_ms, round_limit, round, state, verdict,
       next_round_at, created_at, updated_at
FROM exercise
WHERE id = {{id:uuid}}
