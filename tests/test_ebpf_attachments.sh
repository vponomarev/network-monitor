#!/bin/bash
# Detailed eBPF attachment and connection tracking test
# Run with: sudo bash tests/test_ebpf_attachments.sh

set -e

CONNTRACK_BINARY="${CONNTRACK_BINARY:-/usr/local/bin/conntrack}"
LOG_FILE="/tmp/ebpf_test.log"

echo "========================================"
echo "  eBPF Attachment & Connection Test"
echo "========================================"
echo ""

# Check root
if [ "$EUID" -ne 0 ]; then
    echo "Must run as root"
    exit 1
fi

# Stop existing conntrack
systemctl stop conntrack 2>/dev/null || true
pkill -f "conntrack.*--config" 2>/dev/null || true
sleep 1

# Clear logs
> "$LOG_FILE"

echo "Starting conntrack with debug logging..."
$CONNTRACK_BINARY --config /dev/null 2>&1 | tee -a "$LOG_FILE" &
PID=$!
sleep 3

echo ""
echo "=== eBPF Attachment Status ==="
grep -E "Loading|Attached|loaded successfully" "$LOG_FILE" || echo "No attachment info found"

echo ""
echo "=== Testing Outgoing Connections ==="

# Test outgoing: connect to local port
echo "Creating outgoing connection to port 19999..."
nc -l -p 19999 &
NC_PID=$!
sleep 0.5

echo "test-outgoing" | nc -w 1 127.0.0.1 19999 2>/dev/null || true
sleep 2

kill $NC_PID 2>/dev/null || true

echo ""
echo "=== Outgoing Connection Logs ==="
grep -iE "outgoing|tcp_connect|127.0.0.1:19999|state" "$LOG_FILE" | tail -10 || echo "No outgoing logs found"

echo ""
echo "=== Testing Incoming Connections ==="

# Test incoming
echo "Creating incoming connection to port 19998..."
nc -l -p 19998 &
NC_PID=$!
sleep 0.5

echo "test-incoming" | nc -w 1 127.0.0.1 19998 2>/dev/null || true
sleep 2

kill $NC_PID 2>/dev/null || true

echo ""
echo "=== Incoming Connection Logs ==="
grep -iE "incoming|inet_csk_accept|127.0.0.1:19998|state" "$LOG_FILE" | tail -10 || echo "No incoming logs found"

echo ""
echo "=== All Connection Events ==="
grep -iE "connection|state|event" "$LOG_FILE" | tail -20 || echo "No events found"

# Cleanup
kill $PID 2>/dev/null || true

echo ""
echo "=== Kernel Info ==="
uname -r
cat /sys/kernel/btf/vmlinux 2>/dev/null | head -c 100 && echo " (BTF available)" || echo "BTF not available"

echo ""
echo "Log file: $LOG_FILE"
