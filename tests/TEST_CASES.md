# Conntrack Test Cases

## Overview

This document describes test cases for verifying incoming and outgoing connection tracking functionality.

## Prerequisites

- Root access on test host
- conntrack binary installed
- Kernel 5.15+ recommended (for full eBPF support)
- BTF enabled (`/sys/kernel/btf/vmlinux` exists)

## Test Cases

### TC-001: eBPF Program Attachment

**Purpose:** Verify all eBPF programs attach successfully

**Steps:**
```bash
sudo conntrack --config /etc/conntrack/config.yaml 2>&1 | grep -E "Attached|loaded"
```

**Expected:**
- `eBPF collection loaded successfully`
- `Attached kprobe/tcp_connect for outgoing connections`
- `Attached kretprobe/inet_csk_accept for incoming connections`
- `Attached kprobe/tcp_close for connection closing`

### TC-002: Outgoing Connection Tracking

**Purpose:** Verify outgoing TCP connections are tracked

**Steps:**
```bash
# Start conntrack
sudo conntrack --config /etc/conntrack/config.yaml 2>&1 | tee /tmp/conntrack.log &

# Generate outgoing connection
nc -l -p 19999 &
echo "test" | nc -w 1 127.0.0.1 19999

# Wait for events
sleep 2

# Check logs
grep -i "outgoing\|tcp_connect\|Parsed" /tmp/conntrack.log
```

**Expected:**
- Event logged with direction=outgoing
- Source/destination IP and ports captured
- Process name identified

### TC-003: Incoming Connection Tracking

**Purpose:** Verify incoming TCP connections are tracked

**Steps:**
```bash
# Start conntrack
sudo conntrack --config /etc/conntrack/config.yaml 2>&1 | tee /tmp/conntrack.log &

# Start server
nc -l -p 19998 &

# Generate incoming connection
echo "test" | nc -w 1 127.0.0.1 19998

# Wait for events
sleep 2

# Check logs
grep -i "incoming\|inet_csk_accept\|Parsed" /tmp/conntrack.log
```

**Expected:**
- Event logged with direction=incoming
- Source/destination IP and ports captured
- Server process name identified

### TC-004: Connection Close Tracking

**Purpose:** Verify connection close events are tracked

**Steps:**
```bash
# Start conntrack with debug logging
sudo conntrack --config /etc/conntrack/config.yaml 2>&1 | tee /tmp/conntrack.log &

# Create and close connection
nc -l -p 19997 &
echo "test" | nc -w 1 127.0.0.1 19997
sleep 1

# Check for close events
grep -i "tcp_close\|closed" /tmp/conntrack.log
```

**Expected:**
- Close event logged
- Connection state updated to CLOSED

### TC-005: Process Identification

**Purpose:** Verify process name is captured for connections

**Steps:**
```bash
# Start conntrack
sudo conntrack --config /etc/conntrack/config.yaml 2>&1 | tee /tmp/conntrack.log &

# Create connection from specific process
curl -s --connect-timeout 1 http://127.0.0.1:19996 2>/dev/null || true

# Check process name in logs
grep -i "PID\|process\|comm" /tmp/conntrack.log
```

**Expected:**
- Process name (e.g., "curl", "nc") captured
- PID matches creating process

### TC-006: Full Connection Lifecycle

**Purpose:** Verify full TCP handshake (SYN → SYN+ACK → ESTABLISHED → CLOSE)

**Steps:**
```bash
# Start conntrack
sudo conntrack --config /etc/conntrack/config.yaml 2>&1 | tee /tmp/conntrack.log &

# Server
nc -l -p 19995 &
SERVER_PID=$!

# Client
echo "lifecycle-test" | nc -w 1 127.0.0.1 19995

# Wait for all events
sleep 3

# Check state transitions
grep -iE "state.*SYN|state.*ESTABLISHED|state.*CLOSED" /tmp/conntrack.log
```

**Expected:**
- SYN_SENT → ESTABLISHED → CLOSED transitions logged

### TC-007: Concurrent Connections

**Purpose:** Verify tracking under concurrent load

**Steps:**
```bash
# Start conntrack
sudo conntrack --config /etc/conntrack/config.yaml 2>&1 | tee /tmp/conntrack.log &

# Start server
nc -l -p 19994 &
SERVER_PID=$!

# Create 10 concurrent connections
for i in {1..10}; do
    echo "concurrent-$i" | nc -w 1 127.0.0.1 19994 &
done
wait

# Wait for events
sleep 3

# Count events
grep -c "Parsed" /tmp/conntrack.log
```

**Expected:**
- All 10 connections tracked
- No events lost

## Known Issues

### Outgoing Connections Not Detected

**Symptom:** kprobe/tcp_connect attached but no outgoing events

**Affected Kernels:** Some 6.1.x kernels (Debian 12)

**Workaround:** Build with fallback mode:
```bash
cd bpf
make fallback
```

Or use tracepoint mode which is more reliable on newer kernels.

### Process Name Empty

**Symptom:** Process name shows as "unknown"

**Cause:** Race condition between eBPF event and process exit

**Fix:** Use `/proc/{pid}/comm` fallback (implemented in tracker_linux.go)

## Test Results Template

```markdown
### Test Environment
- Host: <hostname>
- Kernel: <uname -r>
- OS: <cat /etc/os-release>
- conntrack version: <conntrack --version>

### Results
| Test Case | Status | Notes |
|-----------|--------|-------|
| TC-001    | PASS/FAIL | |
| TC-002    | PASS/FAIL | |
| TC-003    | PASS/FAIL | |
| TC-004    | PASS/FAIL | |
| TC-005    | PASS/FAIL | |
| TC-006    | PASS/FAIL | |
| TC-007    | PASS/FAIL | |

### Logs
<Attach relevant log excerpts>
```

## Running All Tests

```bash
# Automated test suite
sudo bash tests/run_conntrack_tests.sh

# Individual test
sudo bash tests/test_simple.sh /path/to/conntrack /path/to/config.yaml
```
