#!/bin/sh
set -eu

if [ "$(id -u)" -ne 0 ]; then
    echo "ERROR: run as root" >&2
    exit 1
fi

BINARY=${1:-}
DURATION=${CONNTRACK_SOAK_DURATION_SECONDS:-1800}
SAMPLE_INTERVAL=${CONNTRACK_SOAK_SAMPLE_INTERVAL_SECONDS:-10}
TRAFFIC_INTERVAL=${CONNTRACK_SOAK_TRAFFIC_INTERVAL_SECONDS:-1}
MAX_RSS_KB=${CONNTRACK_SOAK_MAX_RSS_KB:-262144}
MAX_CPU_PERCENT=${CONNTRACK_SOAK_MAX_CPU_PERCENT:-25}
MAX_DROP_DELTA=${CONNTRACK_SOAK_MAX_DROP_DELTA:-0}

if [ -z "$BINARY" ] || [ ! -x "$BINARY" ]; then
    echo "Usage: $0 /path/to/conntrack-linux-amd64" >&2
    exit 2
fi

case "$DURATION:$SAMPLE_INTERVAL:$TRAFFIC_INTERVAL" in
    *[!0-9:]*|0:*|*:0:*|*:0)
        echo "ERROR: duration and intervals must be positive integer seconds" >&2
        exit 2
        ;;
esac

for command in curl python3 ps awk; do
    command -v "$command" >/dev/null 2>&1 || {
        echo "ERROR: required command is missing: $command" >&2
        exit 2
    }
done

free_port() {
    python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()'
}

WORKDIR=$(mktemp -d /tmp/conntrack-soak.XXXXXX)
CONFIG="$WORKDIR/config.yaml"
LOG="$WORKDIR/conntrack.log"
METRICS_PORT=$(free_port)
TRAFFIC_PORT=$(free_port)
while [ "$TRAFFIC_PORT" = "$METRICS_PORT" ]; do
    TRAFFIC_PORT=$(free_port)
done
CONNTRACK_PID=
HTTP_PID=
TRAFFIC_PID=

cleanup() {
    status=$?
    for pid in "$TRAFFIC_PID" "$CONNTRACK_PID" "$HTTP_PID"; do
        [ -z "$pid" ] || kill -TERM "$pid" 2>/dev/null || true
    done
    for pid in "$TRAFFIC_PID" "$CONNTRACK_PID" "$HTTP_PID"; do
        [ -z "$pid" ] || wait "$pid" 2>/dev/null || true
    done
    if [ "$status" -ne 0 ]; then
        tail -100 "$LOG" >&2 2>/dev/null || true
    fi
    if [ "${CONNTRACK_SOAK_KEEP:-0}" = "1" ]; then
        echo "Soak artifacts: $WORKDIR"
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
  level: info
  format: json
EOF

python3 -m http.server "$TRAFFIC_PORT" --bind 127.0.0.1 --directory "$WORKDIR" >/dev/null 2>&1 &
HTTP_PID=$!
"$BINARY" --config "$CONFIG" >"$LOG" 2>&1 &
CONNTRACK_PID=$!

elapsed=0
until curl -fsS -o /dev/null "http://127.0.0.1:$METRICS_PORT/ready" 2>/dev/null; do
    kill -0 "$CONNTRACK_PID" 2>/dev/null || {
        echo "ERROR: conntrack exited before readiness" >&2
        exit 1
    }
    elapsed=$((elapsed + 1))
    [ "$elapsed" -lt 30 ] || {
        echo "ERROR: readiness timeout" >&2
        exit 1
    }
    sleep 1
done

(
    while kill -0 "$CONNTRACK_PID" 2>/dev/null; do
        curl -fsS -o /dev/null "http://127.0.0.1:$TRAFFIC_PORT/" 2>/dev/null || true
        sleep "$TRAFFIC_INTERVAL"
    done
) &
TRAFFIC_PID=$!

metric_sum() {
    name=$1
    curl -fsS "http://127.0.0.1:$METRICS_PORT/metrics" |
        awk -v metric="$name" '$1 == metric || index($1, metric "{") == 1 { sum += $2 } END { printf "%.0f", sum + 0 }'
}

BASE_DROPS=$(metric_sum conntrack_dropped_events_total)
MAX_RSS_SEEN=0
MAX_CPU_SEEN=0
MAX_STATE_SEEN=0
START=$(date +%s)
END=$((START + DURATION))

while [ "$(date +%s)" -lt "$END" ]; do
    kill -0 "$CONNTRACK_PID" 2>/dev/null || {
        echo "ERROR: conntrack exited during soak" >&2
        exit 1
    }
    curl -fsS -o /dev/null "http://127.0.0.1:$METRICS_PORT/ready"
    RSS=$(awk '/VmRSS:/ { print $2 }' "/proc/$CONNTRACK_PID/status")
    CPU=$(ps -p "$CONNTRACK_PID" -o %cpu= | awk '{ print $1 + 0 }')
    STATE=$(metric_sum conntrack_state_entries)
    [ "$RSS" -le "$MAX_RSS_SEEN" ] || MAX_RSS_SEEN=$RSS
    MAX_CPU_SEEN=$(awk -v a="$MAX_CPU_SEEN" -v b="$CPU" 'BEGIN { print (a > b ? a : b) }')
    [ "$STATE" -le "$MAX_STATE_SEEN" ] || MAX_STATE_SEEN=$STATE
    sleep "$SAMPLE_INTERVAL"
done

FINAL_DROPS=$(metric_sum conntrack_dropped_events_total)
DROP_DELTA=$((FINAL_DROPS - BASE_DROPS))

[ "$MAX_RSS_SEEN" -le "$MAX_RSS_KB" ] || {
    echo "FAIL: max RSS ${MAX_RSS_SEEN}KB exceeds ${MAX_RSS_KB}KB" >&2
    exit 1
}
awk -v actual="$MAX_CPU_SEEN" -v limit="$MAX_CPU_PERCENT" 'BEGIN { exit !(actual <= limit) }' || {
    echo "FAIL: max CPU ${MAX_CPU_SEEN}% exceeds ${MAX_CPU_PERCENT}%" >&2
    exit 1
}
[ "$DROP_DELTA" -le "$MAX_DROP_DELTA" ] || {
    echo "FAIL: drop delta $DROP_DELTA exceeds $MAX_DROP_DELTA" >&2
    exit 1
}
[ "$MAX_STATE_SEEN" -le 36864 ] || {
    echo "FAIL: state entries $MAX_STATE_SEEN exceed configured aggregate limits" >&2
    exit 1
}

kill -TERM "$CONNTRACK_PID"
wait "$CONNTRACK_PID"
CONNTRACK_PID=

printf 'PASS\tduration_s=%s\tmax_rss_kb=%s\tmax_cpu_pct=%s\tmax_state=%s\tdrop_delta=%s\tkernel=%s\n' \
    "$DURATION" "$MAX_RSS_SEEN" "$MAX_CPU_SEEN" "$MAX_STATE_SEEN" "$DROP_DELTA" "$(uname -r)"
