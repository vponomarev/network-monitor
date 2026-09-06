#!/bin/sh
set -eu

if [ "$(id -u)" -ne 0 ]; then
    echo "ERROR: run as root (eBPF load and attach require privileges)" >&2
    exit 1
fi

BINARY=${1:-}
TIMEOUT=${CONNTRACK_E2E_TIMEOUT:-20}

if [ -z "$BINARY" ] || [ ! -x "$BINARY" ]; then
    echo "Usage: $0 /path/to/conntrack-linux-amd64" >&2
    exit 2
fi

for command in curl python3; do
    command -v "$command" >/dev/null 2>&1 || {
        echo "ERROR: required command is missing: $command" >&2
        exit 2
    }
done

free_port() {
    python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()'
}

METRICS_PORT=${CONNTRACK_E2E_METRICS_PORT:-$(free_port)}
TRAFFIC_PORT=${CONNTRACK_E2E_TRAFFIC_PORT:-$(free_port)}
while [ "$TRAFFIC_PORT" = "$METRICS_PORT" ]; do
    TRAFFIC_PORT=$(free_port)
done

WORKDIR=$(mktemp -d /tmp/conntrack-e2e.XXXXXX)
CONFIG="$WORKDIR/config.yaml"
LOG="$WORKDIR/conntrack.log"
HTTP_LOG="$WORKDIR/http.log"
CONNTRACK_PID=
HTTP_PID=

cleanup() {
    status=$?
    [ -z "$CONNTRACK_PID" ] || kill -TERM "$CONNTRACK_PID" 2>/dev/null || true
    [ -z "$HTTP_PID" ] || kill -TERM "$HTTP_PID" 2>/dev/null || true
    [ -z "$CONNTRACK_PID" ] || wait "$CONNTRACK_PID" 2>/dev/null || true
    [ -z "$HTTP_PID" ] || wait "$HTTP_PID" 2>/dev/null || true
    if [ "$status" -ne 0 ]; then
        echo "--- conntrack log (failure) ---" >&2
        tail -100 "$LOG" >&2 2>/dev/null || true
    fi
    if [ "${CONNTRACK_E2E_KEEP:-0}" = "1" ]; then
        echo "E2E artifacts: $WORKDIR"
    else
        rm -rf "$WORKDIR"
    fi
    exit "$status"
}
trap cleanup EXIT INT TERM

cat >"$CONFIG" <<EOF
global:
  metrics_addr: 127.0.0.1
  metrics_port: $METRICS_PORT
connections:
  enabled: true
  track_incoming: true
  track_outgoing: true
  event_buffer_size: 10000
  state_ttl: 24h
  cleanup_interval: 1m
  max_tracked_connections: 10240
  max_pending_connections: 16384
logging:
  level: debug
  format: json
EOF

python3 -m http.server "$TRAFFIC_PORT" --bind 127.0.0.1 --directory "$WORKDIR" >"$HTTP_LOG" 2>&1 &
HTTP_PID=$!
"$BINARY" --config "$CONFIG" >"$LOG" 2>&1 &
CONNTRACK_PID=$!

wait_for() {
    description=$1
    shift
    elapsed=0
    until "$@"; do
        if ! kill -0 "$CONNTRACK_PID" 2>/dev/null; then
            echo "ERROR: conntrack exited while waiting for $description" >&2
            return 1
        fi
        elapsed=$((elapsed + 1))
        if [ "$elapsed" -ge "$TIMEOUT" ]; then
            echo "ERROR: timed out waiting for $description" >&2
            return 1
        fi
        sleep 1
    done
}

wait_for 'test HTTP server' curl -fs -o /dev/null "http://127.0.0.1:$TRAFFIC_PORT/"
wait_for readiness curl -fsS -o /dev/null "http://127.0.0.1:$METRICS_PORT/ready"
curl -fsS "http://127.0.0.1:$METRICS_PORT/health" | grep -qx 'ok'

i=0
while [ "$i" -lt 3 ]; do
    curl -fsS "http://127.0.0.1:$TRAFFIC_PORT/" >/dev/null
    i=$((i + 1))
done

wait_for 'outgoing CLOSE event' grep -q '"direction":"outgoing".*"state":"CLOSED"' "$LOG"
wait_for 'incoming CLOSE event' grep -q '"direction":"incoming".*"state":"CLOSED"' "$LOG"

python3 - "$LOG" <<'PY'
import json
import sys

events = []
with open(sys.argv[1], encoding="utf-8") as stream:
    for line in stream:
        try:
            item = json.loads(line)
        except json.JSONDecodeError:
            continue
        if item.get("msg") == "Connection event":
            events.append(item)

for direction in ("outgoing", "incoming"):
    established = [e for e in events if e.get("direction") == direction and e.get("state") == "ESTABLISHED"]
    closed = [e for e in events if e.get("direction") == direction and e.get("state") == "CLOSED"]
    if not established or not closed:
        raise SystemExit(f"missing {direction} ESTABLISHED/CLOSED lifecycle")
    closed_tuples = {(e.get("source"), e.get("src_port"), e.get("dest"), e.get("dst_port")) for e in closed}
    if not any((e.get("source"), e.get("src_port"), e.get("dest"), e.get("dst_port")) in closed_tuples for e in established):
        raise SystemExit(f"{direction} ESTABLISHED and CLOSED tuples do not match")

outgoing = [e for e in events if e.get("direction") == "outgoing" and e.get("state") == "ESTABLISHED"]
if not any(e.get("pid", 0) > 0 and e.get("process") not in (None, "", "unknown") for e in outgoing):
    raise SystemExit("outgoing ESTABLISHED event has no PID/comm correlation")
PY

metrics_contain() {
    curl -fsS "http://127.0.0.1:$METRICS_PORT/metrics" | grep -q "$1"
}

# State/drop gauges are refreshed by a periodic tracker ticker.
wait_for 'conntrack drop metrics' metrics_contain 'conntrack_dropped_events_total{reason="ringbuf_full"}'
METRICS=$(curl -fsS "http://127.0.0.1:$METRICS_PORT/metrics")
for direction in outgoing incoming; do
    printf '%s\n' "$METRICS" | awk -v d="$direction" '$1 == "conntrack_events_total{direction=\"" d "\",event=\"ESTABLISHED\"}" && $2 >= 3 { found=1 } END { exit !found }'
done
printf '%s\n' "$METRICS" | grep -q 'conntrack_dropped_events_total{reason="ringbuf_full"}'
printf '%s\n' "$METRICS" | grep -q 'conntrack_dropped_events_total{reason="event_channel_full"}'
printf '%s\n' "$METRICS" | grep -q 'conntrack_state_entries{layer="userspace"}'
printf '%s\n' "$METRICS" | grep -q 'conntrack_state_cleanup_total{reason="ttl"}'
printf '%s\n' "$METRICS" | grep -q 'conntrack_state_evictions_total{layer="userspace"}'
printf '%s\n' "$METRICS" | grep -q 'conntrack_state_overflow_total{layer="kernel_connections"}'

kill -TERM "$CONNTRACK_PID"
wait "$CONNTRACK_PID"
CONNTRACK_PID=

printf 'PASS\thost=%s\tos=%s\tkernel=%s\n' \
    "$(hostname)" \
    "$(. /etc/os-release 2>/dev/null; printf '%s' "${PRETTY_NAME:-unknown}")" \
    "$(uname -r)"
