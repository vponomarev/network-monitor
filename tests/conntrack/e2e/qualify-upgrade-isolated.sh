#!/bin/sh
set -eu

if [ "$(id -u)" -ne 0 ]; then
    echo "ERROR: run as root" >&2
    exit 1
fi

GOOD_CANDIDATE=${1:-}
BAD_CANDIDATE=${2:-}
METRICS_PORT=${CONNTRACK_E2E_METRICS_PORT:-19877}
if [ -z "$GOOD_CANDIDATE" ] || [ ! -x "$GOOD_CANDIDATE" ] ||
   [ -z "$BAD_CANDIDATE" ] || [ ! -x "$BAD_CANDIDATE" ]; then
    echo "Usage: $0 /path/to/good-candidate /path/to/bad-unit-candidate" >&2
    exit 2
fi

WORKDIR=$(mktemp -d /tmp/conntrack-upgrade-isolated.XXXXXX)
WAS_ACTIVE=$(systemctl is-active conntrack.service 2>/dev/null || true)
WAS_ENABLED=$(systemctl is-enabled conntrack.service 2>/dev/null || true)

backup_file() {
    source=$1
    name=$2
    if [ -e "$source" ]; then
        cp -a "$source" "$WORKDIR/$name"
        : >"$WORKDIR/$name.present"
    fi
}
backup_file /usr/local/bin/conntrack binary
backup_file /etc/conntrack/config.yaml config
backup_file /etc/systemd/system/conntrack.service unit
if [ -d /var/lib/conntrack/rollback ]; then
    cp -a /var/lib/conntrack/rollback "$WORKDIR/rollback"
fi

restore_host() {
    status=$?
    trap - EXIT INT TERM
    systemctl stop conntrack.service >/dev/null 2>&1 || true
    rm -f /usr/local/bin/conntrack /etc/conntrack/config.yaml /etc/systemd/system/conntrack.service
    rm -rf /var/lib/conntrack/rollback
    [ ! -f "$WORKDIR/binary.present" ] || cp -a "$WORKDIR/binary" /usr/local/bin/conntrack
    [ ! -f "$WORKDIR/config.present" ] || cp -a "$WORKDIR/config" /etc/conntrack/config.yaml
    [ ! -f "$WORKDIR/unit.present" ] || cp -a "$WORKDIR/unit" /etc/systemd/system/conntrack.service
    if [ -d "$WORKDIR/rollback" ]; then
        mkdir -p /var/lib/conntrack
        cp -a "$WORKDIR/rollback" /var/lib/conntrack/rollback
    fi
    systemctl daemon-reload >/dev/null 2>&1 || true
    systemctl reset-failed conntrack.service >/dev/null 2>&1 || true
    case "$WAS_ENABLED" in
        enabled|enabled-runtime) systemctl enable conntrack.service >/dev/null 2>&1 || true ;;
        *) systemctl disable conntrack.service >/dev/null 2>&1 || true ;;
    esac
    [ "$WAS_ACTIVE" != "active" ] || systemctl start conntrack.service >/dev/null 2>&1 || true
    rm -rf "$WORKDIR"
    exit "$status"
}
trap restore_host EXIT INT TERM

systemctl stop conntrack.service >/dev/null 2>&1 || true
sed -i "s/^[[:space:]]*metrics_port:.*/  metrics_port: $METRICS_PORT/" /etc/conntrack/config.yaml
sed -i 's#^ExecStart=.*#ExecStart=/usr/local/bin/conntrack --config /etc/conntrack/config.yaml#' \
    /etc/systemd/system/conntrack.service
systemctl daemon-reload
systemctl reset-failed conntrack.service >/dev/null 2>&1 || true
systemctl start conntrack.service

elapsed=0
until systemctl is-active --quiet conntrack.service &&
      curl -fsS -o /dev/null "http://127.0.0.1:$METRICS_PORT/ready"; do
    elapsed=$((elapsed + 1))
    [ "$elapsed" -lt 30 ] || exit 1
    sleep 1
done

CONNTRACK_E2E_METRICS_PORT=$METRICS_PORT \
    tests/conntrack/e2e/qualify-upgrade.sh "$GOOD_CANDIDATE"
CONNTRACK_E2E_METRICS_PORT=$METRICS_PORT \
    tests/conntrack/e2e/qualify-auto-rollback.sh "$BAD_CANDIDATE"

echo "PASS isolated upgrade, repeat install, explicit and automatic rollback"
