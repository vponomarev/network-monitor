#!/bin/sh
set -eu

if [ "$(id -u)" -ne 0 ]; then
    echo "ERROR: run as root" >&2
    exit 1
fi

BAD_CANDIDATE=${1:-}
METRICS_PORT=${CONNTRACK_E2E_METRICS_PORT:-9876}
if [ -z "$BAD_CANDIDATE" ] || [ ! -x "$BAD_CANDIDATE" ]; then
    echo "Usage: $0 /path/to/candidate-with-invalid-unit" >&2
    exit 2
fi

WORKDIR=$(mktemp -d /tmp/conntrack-auto-rollback.XXXXXX)
WAS_ACTIVE=$(systemctl is-active conntrack.service 2>/dev/null || true)
cp -a /usr/local/bin/conntrack "$WORKDIR/binary"
cp -a /etc/conntrack/config.yaml "$WORKDIR/config"
cp -a /etc/systemd/system/conntrack.service "$WORKDIR/unit"

restore_host() {
    status=$?
    trap - EXIT INT TERM
    systemctl stop conntrack.service >/dev/null 2>&1 || true
    cp -a "$WORKDIR/binary" /usr/local/bin/conntrack
    cp -a "$WORKDIR/config" /etc/conntrack/config.yaml
    cp -a "$WORKDIR/unit" /etc/systemd/system/conntrack.service
    rm -rf /var/lib/conntrack/rollback
    systemctl daemon-reload >/dev/null 2>&1 || true
    [ "$WAS_ACTIVE" != "active" ] || systemctl start conntrack.service >/dev/null 2>&1 || true
    rm -rf "$WORKDIR"
    exit "$status"
}
trap restore_host EXIT INT TERM

systemctl start conntrack.service
elapsed=0
until curl -fsS -o /dev/null "http://127.0.0.1:$METRICS_PORT/ready"; do
    elapsed=$((elapsed + 1))
    [ "$elapsed" -lt 30 ] || exit 1
    sleep 1
done

BASE_BINARY=$(sha256sum /usr/local/bin/conntrack | cut -c1-64)
BASE_UNIT=$(sha256sum /etc/systemd/system/conntrack.service | cut -c1-64)
if "$BAD_CANDIDATE" install; then
    echo "ERROR: invalid candidate unexpectedly installed" >&2
    exit 1
fi

systemctl is-active --quiet conntrack.service
curl -fsS -o /dev/null "http://127.0.0.1:$METRICS_PORT/ready"
[ "$(sha256sum /usr/local/bin/conntrack | cut -c1-64)" = "$BASE_BINARY" ]
[ "$(sha256sum /etc/systemd/system/conntrack.service | cut -c1-64)" = "$BASE_UNIT" ]
test ! -e /var/lib/conntrack/rollback

echo "PASS automatic rollback after failed restart/readiness"
