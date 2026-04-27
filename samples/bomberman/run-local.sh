#!/usr/bin/env bash
# samples/bomberman/run-local.sh — start the full bomberman cluster
# (inspector + 3 nodes + 1 gate + static web), tear it down on Ctrl+C.
#
# Prerequisites running on localhost:
#   - Postgres on :5432 (docker compose up -d postgres)
#   - etcd on :2379 (docker compose up -d etcd)
#
# Override match length with BOMBERMAN_MATCH_TIME_LIMIT_TICKS (default
# here is 300 ticks / 30 s for snappy local play; production default
# in the binary is 1800 / 3 min).
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT"

CONFIG="samples/bomberman/stress_config.yml"
LOG_DIR="${TMPDIR:-/tmp}/bomberman-run"
mkdir -p "$LOG_DIR"

export BOMBERMAN_MATCH_TIME_LIMIT_TICKS="${BOMBERMAN_MATCH_TIME_LIMIT_TICKS:-300}"

PIDS=()

cleanup() {
  echo
  echo "--- shutting down ---"
  for pid in "${PIDS[@]}"; do
    if kill -0 "$pid" 2>/dev/null; then
      kill -TERM "$pid" 2>/dev/null || true
    fi
  done
  # Brief grace for graceful exit, then SIGKILL stragglers.
  sleep 2
  for pid in "${PIDS[@]}"; do
    if kill -0 "$pid" 2>/dev/null; then
      kill -KILL "$pid" 2>/dev/null || true
    fi
  done
}
trap cleanup INT TERM EXIT

require_port() {
  local host=$1 port=$2 label=$3
  if ! (echo > "/dev/tcp/$host/$port") >/dev/null 2>&1; then
    echo "❌ $label not reachable at $host:$port"
    exit 1
  fi
  echo "✅ $label reachable on $host:$port"
}

require_port 127.0.0.1 5432 "Postgres"
require_port 127.0.0.1 2379 "etcd"

echo
echo "=== compile protos ==="
./script/compile-proto.sh > "$LOG_DIR/compile-proto.log" 2>&1
echo "    log: $LOG_DIR/compile-proto.log"

echo
echo "=== init Postgres schema ==="
go run ./cmd/pgadmin --config "$CONFIG" init

start_bg() {
  local label=$1
  shift
  echo "→ $label  (log: $LOG_DIR/$label.log)"
  "$@" > "$LOG_DIR/$label.log" 2>&1 &
  PIDS+=("$!")
}

echo
echo "=== start cluster ==="
start_bg inspector  go run ./cmd/inspector --config "$CONFIG"
sleep 2
start_bg node-1     go run ./samples/bomberman/server --config "$CONFIG" --node-id bomberman-node-1
start_bg node-2     go run ./samples/bomberman/server --config "$CONFIG" --node-id bomberman-node-2
start_bg node-3     go run ./samples/bomberman/server --config "$CONFIG" --node-id bomberman-node-3
sleep 4
start_bg gate       go run ./cmd/gate --config "$CONFIG" --gate-id bomberman-gate-1
sleep 2
start_bg web        python3 -m http.server 8000 --directory samples/bomberman/web

echo
echo "================================================================"
echo " ✅ Bomberman cluster up."
echo "    Inspector UI:  http://localhost:8190"
echo "    Game URL:      http://localhost:8000"
echo "    Match length:  ${BOMBERMAN_MATCH_TIME_LIMIT_TICKS} ticks (~$((BOMBERMAN_MATCH_TIME_LIMIT_TICKS / 10)) s)"
echo
echo " Open the game URL in two browser tabs, queue up in both, play."
echo " Logs in ${LOG_DIR}/."
echo " Ctrl+C to shut down."
echo "================================================================"

# Block until interrupted; cleanup() runs from the trap.
wait
