# shellcheck shell=bash
# lib.sh holds what the validation scripts share: a run directory with the built binaries and a
# log per server instance, the service lifecycle, the exercise's API, and the assertions. A
# script sources it under set -euo pipefail.

root=$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --show-toplevel)
cd "$root" || exit 1
run=$(mktemp -d)
echo "run directory: $run"
mkdir -p "$run/bin"

declare -A PIDS=()
BG=()
FAILS=0
PASSES=0

pass() { PASSES=$((PASSES + 1)); echo "PASS $*"; }
fail() { FAILS=$((FAILS + 1)); echo "FAIL $*"; }
note() { echo "     $*"; }

# assert runs a test command and records its result under msg.
assert() {
  local msg=$1
  shift
  if "$@"; then pass "$msg"; else fail "$msg"; fi
}

summary() {
  echo "---"
  echo "summary: $PASSES passed, $FAILS failed (logs in $run)"
  [ "$FAILS" -eq 0 ]
}

build() {
  local svc
  for svc in exercise intelligence command operations; do
    (cd "services/$svc" && go build -o "$run/bin/$svc" ./cmd/server)
  done
  go build -o "$run/bin/courier" ./courier/cmd/courier
}

# fresh_stack is mise run reset then mise run up: every run starts from empty databases and an
# empty stream, so no earlier exercise is replayed.
fresh_stack() {
  docker compose down -v
  docker compose up -d --wait
}

alive() {
  local state
  state=$(ps -o stat= -p "$1" 2>/dev/null) || return 1
  # A child that exited but is not reaped yet still answers kill -0; its state is Z.
  [[ -n $state && $state != Z* ]]
}

# start_svc runs a server from its own directory, so its config files resolve. The subshell
# execs env, and env execs the server, so $! is the server's PID and a signal reaches it.
start_svc() {
  local svc=$1 instance=$2 log="$run/$2.log" pid i
  shift 2
  echo "=== start $instance $(date -Is)" >>"$log"
  (cd "services/$svc" && exec env "${svc^^}_ENV=local" "$@" "$run/bin/$svc") >>"$log" 2>&1 &
  pid=$!
  for i in $(seq 50); do
    [ "$(readlink "/proc/$pid/exe" 2>/dev/null)" = "$run/bin/$svc" ] && break
    [ "$i" -eq 50 ] && { echo "$instance (pid $pid) is not $run/bin/$svc"; exit 1; }
    sleep 0.1
  done
  PIDS[$instance]=$pid
}

# since_start prints an instance's log from its latest start marker.
since_start() {
  awk '/^=== start /{buf=""; next} {buf = buf $0 "\n"} END{printf "%s", buf}' "$run/$1.log"
}

# stop_svc signals an instance and waits, bounded, for it to exit; STOP_ELAPSED is how long it
# took, in milliseconds. A TERM must end in a drain: the server stopped, and no service failed.
stop_svc() {
  local instance=$1 sig=$2 pid=${PIDS[$1]} start=$SECONDS t0
  t0=$(date +%s%3N)
  kill "-$sig" "$pid"
  while alive "$pid"; do
    if [ $((SECONDS - start)) -ge 20 ]; then
      fail "$instance exits within 20s of SIG$sig"
      kill -KILL "$pid" 2>/dev/null || true
      break
    fi
    sleep 0.1
  done
  wait "$pid" 2>/dev/null || true
  STOP_ELAPSED=$(($(date +%s%3N) - t0))
  unset "PIDS[$instance]"
  if [ "$sig" = TERM ]; then
    assert "$instance drained on SIGTERM: server stopped, no service failed (${STOP_ELAPSED}ms)" drained "$instance"
  fi
}

drained() {
  local tail
  tail=$(since_start "$1")
  grep -q 'msg="server stopped"' <<<"$tail" && ! grep -q 'msg="service failed"' <<<"$tail"
}

stop_all() {
  local instance pid
  for instance in "${!PIDS[@]}"; do
    kill -TERM "${PIDS[$instance]}" 2>/dev/null || true
  done
  for pid in "${PIDS[@]}" "${BG[@]}"; do
    kill -TERM "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
  done
}
trap stop_all EXIT

code() { curl -s -o /dev/null -w '%{http_code}' --max-time 2 "localhost:$1$2" || true; }

wait_ready() {
  local port start=$SECONDS
  for port in "$@"; do
    until [ "$(code "$port" /readyz)" = 200 ]; do
      [ $((SECONDS - start)) -ge 60 ] && { echo "no service is ready on :$port within 60s"; exit 1; }
      sleep 0.2
    done
  done
}

create_exercise() {
  local fixture="services/exercise/fixtures/${FIXTURE:-skirmish}.json" body
  body=$(jq -c --argjson seed "${SEED:-null}" 'if $seed == null then . else . + {seed: $seed} end' "$fixture")
  curl -sf -XPOST localhost:8081/api/exercises -d "$body" | jq -r .id
}

# start_exercise starts the narration first, as demo-theater does, so it reads from round 0,
# then starts the exercise. The narration goes to $run/theater-<id>.txt.
start_exercise() {
  "$run/bin/courier" --broker nats scenario theater --exercise "$1" --wait 10m >"$run/theater-$1.txt" 2>&1 &
  BG+=($!)
  NARRATION=$!
  sleep 2
  curl -sf -o /dev/null -XPOST "localhost:8081/api/exercises/$1/start"
}

# view reads a field of the umpire's view, empty when exercise is down.
view() { curl -sf --max-time 2 "localhost:8081/api/exercises/$1" 2>/dev/null | jq -r ".$2" 2>/dev/null || true; }
round() { view "$1" round; }
status() { view "$1" status; }

wait_round() {
  local id=$1 n=$2 start=$SECONDS r
  while :; do
    r=$(round "$id")
    [ -n "$r" ] && [ "$r" -ge "$n" ] && return 0
    [ $((SECONDS - start)) -ge 90 ] && { fail "exercise $id reaches round $n within 90s (at ${r:-?})"; return 1; }
    sleep 0.2
  done
}

wait_concluded() {
  local id=$1 start=$SECONDS
  until [ "$(status "$id")" = concluded ]; do
    [ $((SECONDS - start)) -ge 120 ] && return 1
    sleep 0.5
  done
}

# wait_narration waits, bounded, for the narration to finish after the conclusion and prints its
# verdict line.
wait_narration() {
  local id=$1 start=$SECONDS
  while alive "$NARRATION"; do
    [ $((SECONDS - start)) -ge 30 ] && { note "the narration of $id did not finish within 30s"; return; }
    sleep 0.5
  done
  wait "$NARRATION" 2>/dev/null || note "the narration of $id exited non-zero (see $run/theater-$id.txt)"
  note "narration: $(grep -m1 -E '^\s*verdict ' "$run/theater-$id.txt" | sed 's/^ *//' || echo 'no verdict line')"
}

# check reconciles a run's assessments with exercise's history through courier's theater-check,
# which exits non-zero and lists the inconsistencies when any are found.
check() {
  local out="$run/check-$1.txt"
  "$run/bin/courier" --broker nats scenario theater-check --exercise "$1" >"$out" 2>&1 &&
    grep -q 'consistent  every assessment' "$out" && ! grep -q inconsistenc "$out"
}

# mark writes a named marker into an instance's log, so count can read what follows it. The
# server appends to the same file, one write per line, so the marker lands between lines.
mark() { echo "=== mark $2 $(date -Is)" >>"$run/$1.log"; }

# count counts an instance's log lines holding pattern, after the named marker when given.
count() {
  local instance=$1 pattern=$2 after=${3:-}
  if [ -n "$after" ]; then
    awk -v m="=== mark $after " -v p="$pattern" 'index($0, m) == 1 {on = 1; n = 0; next} on && index($0, p) {n++} END {print n + 0}' "$run/$instance.log"
  else
    grep -cF -- "$pattern" "$run/$instance.log" || true
  fi
}

# psql queries a service's database in the compose stack.
psql() { docker compose exec -T postgres psql -U app -d "$1" -Atc "$2"; }
