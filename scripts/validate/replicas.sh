#!/usr/bin/env bash
# Evidence 4 and 5 on the running services: two operations replicas share one delivery group, and
# stopping one mid-exercise drains it while the other carries both factions to the conclusion.
set -euo pipefail
# shellcheck source=scripts/validate/lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
export SEED=${SEED:-7}

ports_free 8081 8082 8083 8084 8085
build
fresh_stack
start_svc exercise exercise
start_svc intelligence intelligence
start_svc command command
start_svc operations operations-a
start_svc operations operations-b OPERATIONS_SERVER_PORT=8085
wait_ready 8081 8082 8083 8084 8085

id=$(create_exercise)
echo "exercise $id, seed $SEED"
start_exercise "$id"
wait_round "$id" 10 || { summary; exit 1; }

a=$(count operations-a 'msg="event consumed"')
b=$(count operations-b 'msg="event consumed"')
note "by round $(round "$id"): operations-a consumed $a, operations-b consumed $b"
assert "operations-a consumed deliveries of the shared groups" test "$a" -gt 0
assert "operations-b consumed deliveries of the shared groups" test "$b" -gt 0

mark operations-a stop-b
stop_svc operations-b TERM
# shutdown_timeout is 10s; the drain must finish within it, plus the 0.1s exit poll.
assert "operations-b drained within the 10s drain timeout (${STOP_ELAPSED}ms)" test "$STOP_ELAPSED" -le 10500
# Orders for a round past this one can come only from operations-a.
stopped=$(round "$id")
note "operations-b stopped at round $stopped"

assert "exercise $id concluded" wait_concluded "$id"
wait_narration "$id"

after=$(count operations-a 'msg="event consumed"' stop-b)
note "operations-a consumed $after deliveries after operations-b stopped"
assert "operations-a kept consuming after operations-b stopped" test "$after" -gt 0
# The log lines carry the exercise as their subject, not the faction, so the faction evidence is
# exercise's record of the orders it received, by faction and round.
factions=$(psql exercise "select string_agg(faction || '=' || n, ' ' order by faction) from (select faction, count(*) n from exercise_orders where exercise_id = '$id' and round > $stopped + 1 group by faction) f")
note "exercise recorded orders after round $((stopped + 1)), by faction: ${factions:-none}"
assert "operations-a alone issued orders for red after the stop" grep -q 'red=' <<<"$factions"
assert "operations-a alone issued orders for blue after the stop" grep -q 'blue=' <<<"$factions"

assert "theater-check finds the run consistent" check "$id"
for instance in exercise intelligence command operations-a operations-b; do
  assert "$instance logged no event refused" test "$(count "$instance" 'msg="event refused"')" -eq 0
done
summary
