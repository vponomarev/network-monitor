#!/bin/bash
#
# Скрипт для запуска на удалённом хосте
# Вызывается из conntrack_full_test.sh
#

set -e

REMOTE_DIR="$1"

export PATH=/usr/local/go/bin:$PATH
cd "$REMOTE_DIR"

echo "=== Step 1: Go modules ==="
rm -f go.sum
go mod tidy 2>&1 || echo "go mod tidy completed with warnings"

echo ""
echo "=== Step 2: Build eBPF ==="

if command -v clang &> /dev/null && [ -d bpf ]; then
    echo "Building eBPF (with tracepoint fallback for outgoing)..."
    # FALLBACK enabled by default - includes trace_outgoing_fallback program
    make -C bpf all 2>&1
    if [ -f bpf/conntrack.bpf.o ] && [ -s bpf/conntrack.bpf.o ]; then
        echo "eBPF: $(ls -lh bpf/conntrack.bpf.o)"
        cp bpf/conntrack.bpf.o pkg/embedded/bpf/conntrack.bpf.o
        echo "Copied to embedded"
    fi
else
    echo "Clang not available"
fi

if [ -s pkg/embedded/bpf/conntrack.bpf.o ]; then
    echo "Embedded eBPF: $(stat -c%s pkg/embedded/bpf/conntrack.bpf.o) bytes"
else
    echo "No embedded eBPF"
fi

echo ""
echo "=== Step 3: Build conntrack ==="
go build -v -o conntrack ./cmd/conntrack 2>&1
echo "Binary: $(ls -lh conntrack)"

echo ""
echo "=== Step 4: Install ==="
mkdir -p "$REMOTE_DIR/bin"
./conntrack install --install-path "$REMOTE_DIR/bin" 2>&1

echo ""
echo "=== Step 5: Start daemon with debug logging ==="

# Create debug config
cat > "$REMOTE_DIR/debug_config.yaml" << 'EOF'
track_outgoing: true
track_incoming: true
track_closes: true
logging:
  level: debug
  output_path: stdout
EOF

# Start conntrack daemon - redirect stderr to log file
LOG_FILE="$REMOTE_DIR/conntrack_events.log"
"$REMOTE_DIR/bin/conntrack" -c "$REMOTE_DIR/debug_config.yaml" --track-incoming --track-outgoing --track-closes --syn-timeout 5s > "$LOG_FILE" 2>&1 &
CONNTRACK_PID=$!
echo "Daemon PID: $CONNTRACK_PID"
echo "Log file: $LOG_FILE"
sleep 5  # Give more time for initialization

if ! kill -0 $CONNTRACK_PID 2>/dev/null; then
    echo "Daemon crashed"
    cat "$LOG_FILE" 2>/dev/null
    exit 1
fi
echo "Daemon running"

echo ""
echo "=== Step 6: Generating traffic ==="

# Wait for eBPF probes to fully attach
echo "Waiting for eBPF probes..."
sleep 5

echo "OUTGOING (curl to :443):"
for i in 1 2 3 4 5; do
    # Use --no-keepalive to force new TCP connection each time
    curl -s --connect-timeout 5 --no-keepalive -o /dev/null -w "  $i: HTTP %{http_code}\n" https://vpnc.ru/ || echo "  $i: Failed"
    sleep 1  # Delay between requests
done

echo ""
echo "INCOMING (nc to :19999):"
# Start fresh listener for each connection
for i in 1 2 3; do
    timeout 3 bash -c "nc -l -p 19999 -q 1 2>/dev/null &" 
    sleep 0.5
    echo "test $i" | timeout 2 nc localhost 19999 2>&1 && echo "  $i: OK" || echo "  $i: attempt"
done

# Wait for events to propagate through ringbuf
echo ""
echo "Waiting for events to process..."
sleep 5

echo ""
echo "=== Step 7: Checking captured connections ==="

# Show captured events from log file
echo "--- Conntrack events log ---"
if [ -f "$LOG_FILE" ]; then
    # Count total lines
    TOTAL_LINES=$(wc -l < "$LOG_FILE")
    echo "Total log lines: $TOTAL_LINES"
    
    echo ""
    echo "--- Connection events (outgoing/incoming) ---"
    grep -i "outgoing\|incoming" "$LOG_FILE" 2>/dev/null | head -20 || echo "No direction events found"
    
    echo ""
    echo "--- TCP events (SYN/ESTABLISHED) ---"
    grep -i "SYN\|ESTABLISHED\|tcp_connect\|inet_csk" "$LOG_FILE" 2>/dev/null | head -20 || echo "No TCP events found"
    
    echo ""
    echo "--- Last 30 log lines ---"
    tail -30 "$LOG_FILE" 2>/dev/null || echo "Log file not readable"
else
    echo "Log file not created"
fi

# Also check journalctl
echo ""
echo "--- Journalctl (if available) ---"
journalctl -u conntrack -n 20 --no-pager 2>/dev/null || echo "No journalctl entries"

echo ""
echo "=== Step 8: Stop ==="
kill -TERM $CONNTRACK_PID 2>/dev/null || true
wait $CONNTRACK_PID 2>/dev/null || true
echo "Stopped"

echo ''
echo '=== Step 9: Deinstall ==='
./conntrack deinstall 2>&1

echo ""
echo "=== TEST COMPLETE ==="
