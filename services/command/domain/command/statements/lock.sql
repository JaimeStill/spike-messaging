--| tier: native
--| transaction: required
--| native: FOR UPDATE, so two replicas handling one faction's assessments act on them one at a time. Port: FOR UPDATE (MySQL, Oracle), UPDLOCK with HOLDLOCK (SQL Server).
-- Returns one faction's direction in an exercise, locked for the rest of the
-- transaction, or no row. The SELECT list is in the order the store's row
-- type scans.
SELECT exercise_id, faction, status, map, round, revision, sequence, decisions, closed_round, updated_at
FROM direction
WHERE exercise_id = {{exercise_id:uuid}} AND faction = {{faction}}
FOR UPDATE
