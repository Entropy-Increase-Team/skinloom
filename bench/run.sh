#!/usr/bin/env bash
# Load test of design 04 §5.4 (ADR 0038) on Linux or macOS: seed a large site, start
# the backend, measure the cold start and idle memory, run bench/k6/game.js.
#
#   bench/run.sh                         # SQLite in ./bench-data
#   DRIVER=postgres DSN=postgres://... bench/run.sh
#
# Needs go and k6 on PATH. TEXTURES, PLAYERS, USERS, DURATION and RATE tune the size
# and the load; OUT (default bench-out) collects the summary and the server log.
set -euo pipefail
cd "$(dirname "$0")/.."

DRIVER=${DRIVER:-sqlite}
DSN=${DSN:-}
DATA=${DATA:-bench-data}
OUT=${OUT:-bench-out}
PORT=${PORT:-17599}
TEXTURES=${TEXTURES:-1000000}
PLAYERS=${PLAYERS:-100000}
USERS=${USERS:-10000}
mkdir -p "$OUT"

go build -o "$OUT/skinloom" ./cmd/skinloom
go run ./internal/tools/benchseed -data "$DATA" -driver "$DRIVER" ${DSN:+-dsn "$DSN"} \
  -textures "$TEXTURES" -players "$PLAYERS" -users "$USERS"

export SKINLOOM_DATA="$DATA" SKINLOOM_SERVER__LISTEN="127.0.0.1:$PORT" SKINLOOM_WEB__MODE=off
if [ "$DRIVER" != sqlite ]; then
  export SKINLOOM_DATABASE__DRIVER="$DRIVER" SKINLOOM_DATABASE__DSN="$DSN"
fi

# wait_ready <pid> <log> <pause>: until /readyz answers; gives up, showing the log, when
# the server exits or is not ready within two minutes.
wait_ready() {
  local deadline=$(( $(date +%s) + 120 ))
  until curl -sf -o /dev/null "http://127.0.0.1:$PORT/readyz"; do
    if ! kill -0 "$1" 2>/dev/null || [ "$(date +%s)" -ge "$deadline" ]; then
      echo "skinloom serve did not become ready:" >&2
      tail -n 50 "$2" >&2
      kill "$1" 2>/dev/null || true
      exit 1
    fi
    sleep "$3"
  done
}

# The first start makes the 4096-bit signing key; the cold start is the second.
"$OUT/skinloom" serve >"$OUT/first-start.log" 2>&1 &
pid=$!
wait_ready "$pid" "$OUT/first-start.log" 0.05
kill "$pid"
wait "$pid" || true

start=$(date +%s%N)
"$OUT/skinloom" serve >"$OUT/server.log" 2>&1 &
pid=$!
trap 'kill $pid 2>/dev/null || true' EXIT
wait_ready "$pid" "$OUT/server.log" 0.01
ready_ms=$(( ($(date +%s%N) - start) / 1000000 ))
sleep 2
rss_kb() { if [ -r "/proc/$pid/status" ]; then awk '/VmRSS/ {print $2}' "/proc/$pid/status"; else ps -o rss= -p "$pid" | tr -d ' '; fi; }
idle_kb=$(rss_kb)

# A missed threshold must not end the script under set -e before the memory summary.
set +e
k6 run -e BASE="http://127.0.0.1:$PORT" -e PLAYERS="$PLAYERS" --summary-export="$OUT/summary.json" bench/k6/game.js | tee "$OUT/k6.txt"
status=${PIPESTATUS[0]}
set -e
after_kb=$(rss_kb)

{
  echo "cold start (process start to /readyz): ${ready_ms} ms (target < 1000)"
  echo "idle memory: $((idle_kb / 1024)) MB (target < 100)"
  echo "memory after the load: $((after_kb / 1024)) MB (64 MiB of it may be the render cache)"
} | tee "$OUT/process.txt"
fail=$status
[ "$ready_ms" -lt 1000 ] || { echo "cold start target missed"; fail=1; }
[ "$((idle_kb / 1024))" -lt 100 ] || { echo "idle memory target missed"; fail=1; }
exit "$fail"
