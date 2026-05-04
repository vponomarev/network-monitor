#!/bin/bash
# Simple connection tracking test
# Run: sudo bash test_simple.sh

set -e

CONNTRACK="${1:-/tmp/test-v154/conntrack}"
CONFIG="${2:-/etc/conntrack/config.yaml}"

echo "=== Conntrack Connection Test ==="
echo "Binary: $CONNTRACK"
echo "Config: $CONFIG"
echo ""

# Stop existing
pkill -f conntrack 2>/dev/null || true
sleep 1

# Start conntrack with debug logging
echo "Starting conntrack..."
$CONNTRACK --config $CONFIG 2>&1 | tee /tmp/conntrack_debug.log &
PID=$!
sleep 3

# Check if running
if ! kill -0 $PID 2>/dev/null; then
    echo "FAIL: conntrack failed to start"
    cat /tmp/conntrack_debug.log
    exit 1
fi
echo "OK: conntrack started (PID: $PID)"

# Show attachment info
echo ""
echo "=== eBPF Attachment ==="
grep -E "Attached|loaded" /tmp/conntrack_debug.log || echo "No attachment info"

# Test outgoing
echo ""
echo "=== Testing Outgoing (port 19999) ==="
nc -l -p 19999 &
NC_PID=$!
sleep 0.5
echo "outgoing-test" | nc -w 1 127.0.0.1 19999 2>/dev/null || true
sleep 2
kill $NC_PID 2>/dev/null || true

echo "Outgoing events:"
grep -iE "outgoing|tcp_connect|127.0.0.1:19999|Parsed|event" /tmp/conntrack_debug.log | tail -10 || echo "No outgoing events"

# Test incoming
echo ""
echo "=== Testing Incoming (port 19998) ==="
nc -l -p 19998 &
NC_PID=$!
sleep 0.5
echo "incoming-test" | nc -w 1 127.0.0.1 19998 2>/dev/null || true
sleep 2
kill $NC_PID 2>/dev/null || true

echo "Incoming events:"
grep -iE "incoming|inet_csk_accept|127.0.0.1:19998|Parsed|event" /tmp/conntrack_debug.log | tail -10 || echo "No incoming events"

# Cleanup
kill $PID 2>/dev/null || true

echo ""
echo "=== Full Log ==="
cat /tmp/conntrack_debug.log | tail -50
