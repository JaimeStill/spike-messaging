#!/usr/bin/env bash
# The principle, and evidence 2 and 3, on the running services: each outage, a service's or the
# broker's, has its own visible effect while every other service keeps serving, and on its
# return the exercise converges. Each target gets a fresh exercise on the same running stack.
set -euo pipefail
# shellcheck source=scripts/validate/lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
export SEED=${SEED:-7}
TARGETS=${TARGETS:-operations command intelligence exercise nats}
# HOLD is the outage's length in seconds: five 1s rounds, by wall time, since exercise may be
# the one down.
HOLD=${HOLD:-5}

declare -A PORT=([exercise]=8081 [intelligence]=8082 [command]=8083 [operations]=8084)
SERVICES="exercise intelligence command operations"

# command emits and consumes, so a kill can land between a commit and its publish, and inside a
# handling; every other service stops with a drain.
signal() { [ "$1" = command ] && echo KILL || echo TERM; }

down() {
  if [ "$1" = nats ]; then docker compose stop nats >/dev/null; else stop_svc "$1" "$(signal "$1")"; fi
}

up() {
  if [ "$1" = nats ]; then docker compose start nats >/dev/null; else start_svc "$1" "$1"; fi
  # shellcheck disable=SC2046
  wait_ready $(for s in $SERVICES; do echo "${PORT[$s]}"; done)
}

# db_round reads the round from exercise's database, which answers while its API is down.
db_round() { psql exercise "select round from exercise where id = '$1'"; }

unpublished() { psql command "select count(*) from messaging_outbox where published_at is null"; }

# serving asserts the other services' readiness, or, with the broker down, records it: readiness
# covers the broker, so only liveness must hold then.
serving() {
  local target=$1 s
  for s in $SERVICES; do
    [ "$s" = "$target" ] && continue
    if [ "$target" = nats ]; then
      note "$s with nats down: /healthz $(code "${PORT[$s]}" /healthz), /readyz $(code "${PORT[$s]}" /readyz)"
      assert "$s stays live with nats down" test "$(code "${PORT[$s]}" /healthz)" = 200
    else
      assert "$s stays ready with $target down" test "$(code "${PORT[$s]}" /readyz)" = 200
    fi
  done
}

outage() {
  local target=$1 id r0 r1 r2 held p0 p1 killed
  echo "=== outage: $target"
  id=$(create_exercise)
  echo "exercise $id, seed $SEED"
  start_exercise "$id"
  wait_round "$id" 5 || return 0
  for s in $SERVICES; do mark "$s" "stop-$target"; done
  r0=$(db_round "$id")
  down "$target"
  killed=$SECONDS
  [ "$target" = command ] && note "command's outbox rows left unpublished by the kill: $(unpublished)"
  held=$SECONDS
  sleep 1
  r1=$(db_round "$id")
  p0=$(count command 'msg="event published"')
  serving "$target"
  if [ "$target" = exercise ]; then
    assert "exercise's API answers nothing while it is down" test "$(code 8081 /healthz)" != 200
  fi
  sleep $((HOLD - (SECONDS - held) > 0 ? HOLD - (SECONDS - held) : 0))
  r2=$(db_round "$id")
  p1=$(count command 'msg="event published"')
  note "round at the stop $r0, 1s later $r1, at the restart $r2"
  case $target in
    exercise)
      assert "the world paused while exercise was down (round $r0 to $r2)" test "$r2" -le $((r0 + 1)) ;;
    nats)
      note "with nats down, the round went from $r0 to $r2" ;;
    *)
      assert "exercise kept resolving with $target down (round $r0 to $r2)" test "$r2" -gt "$r1" ;;
  esac
  if [ "$target" = intelligence ]; then
    note "command published $p0 events 1s into the outage and $p1 at its end"
    assert "command's decisions stopped changing with intelligence down" test "$p1" -eq "$p0"
  fi
  up "$target"
  if [ "$target" = exercise ]; then
    r1=$(round "$id")
    assert "exercise resumed at round $r1 from round $r2, with no jump and no reset" \
      test "$r1" -ge "$r2" -a "$r1" -le $((r2 + 2))
  fi
  assert "exercise $id concluded after the $target outage" wait_concluded "$id"
  wait_narration "$id"
  if [ "$target" = command ]; then
    # A handling in flight at the kill is redelivered once its AckWait, 30s, elapses.
    sleep $((killed + 35 - SECONDS > 0 ? killed + 35 - SECONDS : 0))
    assert "command's outbox holds no unpublished row after its restart" test "$(unpublished)" -eq 0
    note "command published $(since_start command | grep -c 'msg="event published"' || true) events after its restart, republishes of the rows above included"
    for s in $SERVICES; do note "$s claimed $(count "$s" outcome=repeat "stop-$target") repeated deliveries since the kill"; done
  fi
  assert "theater-check finds the $target run consistent" check "$id"
  for s in $SERVICES; do
    assert "$s logged no event refused since the $target stop" test "$(count "$s" 'msg="event refused"' "stop-$target")" -eq 0
  done
}

ports_free 8081 8082 8083 8084
build
fresh_stack
for s in $SERVICES; do start_svc "$s" "$s"; done
wait_ready 8081 8082 8083 8084
for target in $TARGETS; do outage "$target"; done
summary
