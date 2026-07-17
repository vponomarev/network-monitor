#!/bin/sh
set -eu

if [ "$(id -u)" -ne 0 ]; then
    echo "ERROR: run as root" >&2
    exit 1
fi

CANDIDATE=${1:-}
TIMEOUT=${CONNTRACK_E2E_TIMEOUT:-30}
METRICS_PORT=${CONNTRACK_E2E_METRICS_PORT:-9876}
if [ -z "$CANDIDATE" ] || [ ! -x "$CANDIDATE" ]; then
    echo "Usage: $0 /path/to/new/conntrack-linux-amd64" >&2
    exit 2
fi

for command in curl systemctl sha256sum cmp; do
    command -v "$command" >/dev/null 2>&1 || {
        echo "ERROR: required command is missing: $command" >&2
        exit 2
    }
done

for path in /usr/local/bin/conntrack /etc/conntrack/config.yaml /etc/systemd/system/conntrack.service; do
    [ -e "$path" ] || {
        echo "ERROR: active baseline installation is required: $path" >&2
        exit 2
    }
done

systemctl is-active --quiet conntrack.service || {
    echo "ERROR: baseline conntrack service must be active" >&2
    exit 2
}

WORKDIR=$(mktemp -d /tmp/conntrack-upgrade-e2e.XXXXXX)
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
    systemctl start conntrack.service >/dev/null 2>&1 || true
    if [ "${CONNTRACK_E2E_KEEP:-0}" = "1" ]; then
        echo "Upgrade E2E artifacts: $WORKDIR"
    else
        rm -rf "$WORKDIR"
    fi
    exit "$status"
}
trap restore_host EXIT INT TERM

wait_ready() {
    elapsed=0
    until curl -fsS -o /dev/null "http://127.0.0.1:$METRICS_PORT/ready"; do
        elapsed=$((elapsed + 1))
        [ "$elapsed" -lt "$TIMEOUT" ] || return 1
        sleep 1
    done
}

wait_ready
BASE_BINARY=$(sha256sum /usr/local/bin/conntrack | awk '{print $1}')
BASE_CONFIG=$(sha256sum /etc/conntrack/config.yaml | awk '{print $1}')
BASE_UNIT=$(sha256sum /etc/systemd/system/conntrack.service | awk '{print $1}')

"$CANDIDATE" install
wait_ready
[ "$(sha256sum /etc/conntrack/config.yaml | awk '{print $1}')" = "$BASE_CONFIG" ]
test -f /var/lib/conntrack/rollback/manifest.json

# Repeating the same installation must remain safe and preserve the snapshot.
"$CANDIDATE" install
wait_ready
[ "$(sha256sum /etc/conntrack/config.yaml | awk '{print $1}')" = "$BASE_CONFIG" ]

/usr/local/bin/conntrack rollback
wait_ready
[ "$(sha256sum /usr/local/bin/conntrack | awk '{print $1}')" = "$BASE_BINARY" ]
[ "$(sha256sum /etc/conntrack/config.yaml | awk '{print $1}')" = "$BASE_CONFIG" ]
[ "$(sha256sum /etc/systemd/system/conntrack.service | awk '{print $1}')" = "$BASE_UNIT" ]
test ! -e /var/lib/conntrack/rollback

echo "PASS upgrade/reinstall/readiness/config-preservation/rollback"
