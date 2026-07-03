#!/usr/bin/env bash
# Cold-start verify server for simpleBudget (what-if page QA).
#
# Launches the server against a throwaway COPY of data/ so nothing can
# mutate real data, waits on /api/health (no sleeps), and tears down via
# the /killme endpoint (no pkill, no spurious exit-144).
#
# Usage:
#   scripts/whatif-verify.sh start [port]   # default port 8099
#   scripts/whatif-verify.sh stop  [port]
#   scripts/whatif-verify.sh status [port]
#   scripts/whatif-verify.sh log   [port]   # tail server log
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CMD="${1:-start}"
PORT="${2:-8099}"
WORK="/tmp/budget2-whatif-verify-$PORT"
BASE="http://localhost:$PORT"

healthy() { curl -fsS --max-time 2 "$BASE/api/health" >/dev/null 2>&1; }

wait_ready() { # up to 15s
  for _ in $(seq 1 60); do healthy && return 0; sleep 0.25; done
  return 1
}

wait_gone() { # up to 5s
  for _ in $(seq 1 20); do healthy || return 0; sleep 0.25; done
  return 1
}

case "$CMD" in
  start)
    if healthy; then
      echo "Instance already running on :$PORT — stopping it first"
      curl -fsS "$BASE/killme" >/dev/null || true
      wait_gone || { echo "ERROR: old instance on :$PORT won't die"; exit 1; }
    fi
    rm -rf "$WORK"
    mkdir -p "$WORK"
    cp -r "$REPO/data" "$WORK/data"
    echo "Building server..."
    (cd "$REPO" && go build -o "$WORK/budget2-server" ./cmd/server)
    BUDGET_DATA_DIR="$WORK/data" BUDGET_LISTEN_ADDR=":$PORT" \
      "$WORK/budget2-server" >"$WORK/server.log" 2>&1 &
    echo $! > "$WORK/server.pid"
    if wait_ready; then
      echo "READY $BASE  (what-if: $BASE/whatif)"
      echo "data copy: $WORK/data   log: $WORK/server.log"
    else
      echo "ERROR: server not healthy after 15s. Log tail:"
      tail -20 "$WORK/server.log"
      exit 1
    fi
    ;;
  stop)
    if healthy; then
      curl -fsS "$BASE/killme" >/dev/null || true
      wait_gone || { echo "ERROR: instance on :$PORT won't die"; exit 1; }
    fi
    rm -rf "$WORK"
    echo "stopped and cleaned :$PORT"
    ;;
  status)
    if healthy; then echo "RUNNING $BASE"; else echo "not running on :$PORT"; exit 1; fi
    ;;
  log)
    tail -40 "$WORK/server.log"
    ;;
  *)
    echo "usage: $0 {start|stop|status|log} [port]"; exit 2
    ;;
esac
