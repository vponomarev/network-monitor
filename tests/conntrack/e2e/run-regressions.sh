#!/bin/sh
set -eu
CONNTRACK_TEST=$(readlink -f "${1:?usage: run-regressions.sh conntrack.test discovery.test}")
DISCOVERY_TEST=$(readlink -f "${2:?missing discovery.test}")
[ "$(id -u)" = 0 ] || { echo 'Root is required'; exit 1; }
export NETMON_LIVE_TESTS=1
"$CONNTRACK_TEST" -test.v -test.run '^TestAuditLive'
"$DISCOVERY_TEST" -test.v -test.run '^TestAuditLive'
NS="netmon-regression-$$"
trap 'ip netns del "$NS" 2>/dev/null || true' EXIT INT TERM
ip netns add "$NS"
ip -n "$NS" link set lo up
ip netns exec "$NS" tc qdisc add dev lo root netem delay 80ms
# ip netns exec hides tracefs when it remounts sysfs; restore it only in the
# private mount namespace of this test process.
ip netns exec "$NS" sh -c 'mount -t tracefs tracefs /sys/kernel/tracing; AUDIT_NETEM=1 exec "$1" -test.v -test.run "^TestAuditLiveHandshake$"' sh "$CONNTRACK_TEST"
