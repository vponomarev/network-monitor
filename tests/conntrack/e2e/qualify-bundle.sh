#!/bin/sh
set -eu

if [ "$(id -u)" -ne 0 ]; then
    echo "ERROR: run as root" >&2
    exit 1
fi

BUNDLE=${1:-}
TIMEOUT=${CONNTRACK_E2E_TIMEOUT:-20}
if [ -z "$BUNDLE" ] || [ ! -f "$BUNDLE" ]; then
    echo "Usage: $0 conntrack-<version>-linux-amd64.tar.gz" >&2
    exit 2
fi

for command in curl systemctl tar cmp; do
    command -v "$command" >/dev/null 2>&1 || {
        echo "ERROR: required command is missing: $command" >&2
        exit 2
    }
done

WORKDIR=$(mktemp -d /tmp/conntrack-bundle-e2e.XXXXXX)
BACKUP="$WORKDIR/backup"
EXTRACT="$WORKDIR/extract"
mkdir -p "$BACKUP" "$EXTRACT"

WAS_ACTIVE=$(systemctl is-active conntrack.service 2>/dev/null || true)
WAS_ENABLED=$(systemctl is-enabled conntrack.service 2>/dev/null || true)

backup_file() {
    source=$1
    name=$2
    if [ -e "$source" ]; then
        cp -a "$source" "$BACKUP/$name"
        : >"$BACKUP/$name.present"
    fi
}

backup_file /usr/local/bin/conntrack binary
backup_file /etc/conntrack/config.yaml config
backup_file /etc/systemd/system/conntrack.service unit
if [ -d /var/lib/conntrack/rollback ]; then
    cp -a /var/lib/conntrack/rollback "$BACKUP/rollback"
fi

restore_host() {
    status=$?
    trap - EXIT INT TERM
    systemctl stop conntrack.service >/dev/null 2>&1 || true
    systemctl disable conntrack.service >/dev/null 2>&1 || true
    rm -f /usr/local/bin/conntrack /etc/systemd/system/conntrack.service
    rm -f /etc/conntrack/config.yaml
    rm -rf /var/lib/conntrack/rollback

    [ ! -f "$BACKUP/binary.present" ] || cp -a "$BACKUP/binary" /usr/local/bin/conntrack
    if [ -f "$BACKUP/config.present" ]; then
        mkdir -p /etc/conntrack
        cp -a "$BACKUP/config" /etc/conntrack/config.yaml
    fi
    [ ! -f "$BACKUP/unit.present" ] || cp -a "$BACKUP/unit" /etc/systemd/system/conntrack.service
    if [ -d "$BACKUP/rollback" ]; then
        mkdir -p /var/lib/conntrack
        cp -a "$BACKUP/rollback" /var/lib/conntrack/rollback
    fi
    systemctl daemon-reload >/dev/null 2>&1 || true
    case "$WAS_ENABLED" in
        enabled|enabled-runtime) systemctl enable conntrack.service >/dev/null 2>&1 || true ;;
    esac
    [ "$WAS_ACTIVE" != "active" ] || systemctl start conntrack.service >/dev/null 2>&1 || true

    if [ "${CONNTRACK_E2E_KEEP:-0}" = "1" ]; then
        echo "Bundle E2E artifacts: $WORKDIR"
    else
        rm -rf "$WORKDIR"
    fi
    exit "$status"
}
trap restore_host EXIT INT TERM

tar -xzf "$BUNDLE" -C "$EXTRACT"
BINARY=$(find "$EXTRACT" -type f -name conntrack -perm -u+x | head -1)
BUNDLE_CONFIG=$(find "$EXTRACT" -type f -path '*/configs/config.yaml' | head -1)
if [ -z "$BINARY" ] || [ -z "$BUNDLE_CONFIG" ]; then
    echo "ERROR: bundle does not contain conntrack and configs/config.yaml" >&2
    exit 1
fi

systemctl stop conntrack.service >/dev/null 2>&1 || true
systemctl disable conntrack.service >/dev/null 2>&1 || true
rm -f /etc/conntrack/config.yaml

"$BINARY" install
cmp "$BUNDLE_CONFIG" /etc/conntrack/config.yaml
systemctl enable --now conntrack.service

wait_ready() {
    elapsed=0
    until curl -fsS -o /dev/null http://127.0.0.1:9876/ready; do
        elapsed=$((elapsed + 1))
        if [ "$elapsed" -ge "$TIMEOUT" ]; then
            systemctl status conntrack.service --no-pager >&2 || true
            journalctl -u conntrack.service -n 100 --no-pager >&2 || true
            return 1
        fi
        sleep 1
    done
}

wait_ready
curl -fsS http://127.0.0.1:9876/metrics | grep -q '^conntrack_dropped_events_total'
systemctl restart conntrack.service
wait_ready

/usr/local/bin/conntrack deinstall
test ! -e /usr/local/bin/conntrack
test ! -e /etc/systemd/system/conntrack.service
test -f /etc/conntrack/config.yaml
cmp "$BUNDLE_CONFIG" /etc/conntrack/config.yaml

echo "PASS bundle install/start/readiness/metrics/restart/deinstall/config-preservation"
