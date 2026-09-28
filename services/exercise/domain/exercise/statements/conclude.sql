--| tier: standard
--| transaction: required
-- Concludes the exercise after a round whose verdict ended it: the round,
-- the state it left, and the verdict, each rules value as JSON. from is the
-- status the command found, running for a resolved round or created for a
-- start already over; no row changes when the status moved on meanwhile.
UPDATE exercise
SET status = 'concluded', round = {{round:int}}, state = {{state}}, verdict = {{verdict}},
    next_round_at = NULL, updated_at = CURRENT_TIMESTAMP
WHERE id = {{id:uuid}} AND status = {{from}}
