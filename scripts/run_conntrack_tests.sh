#!/bin/bash
# Script to run conntrack tests on remote hosts
# Usage: ./scripts/run_conntrack_tests.sh

set -e

HOSTS=(
    "192.168.5.217"  # Ubuntu 22.04, kernel 5.15.0-177
    "192.168.5.214"  # Debian 13, kernel 6.12.85
    "192.168.5.193"  # Debian 12, kernel 6.1.0-45
    "192.168.5.99"   # Debian 12 + Proxmox, kernel 6.8.12-20-pve
)

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
REMOTE_DIR="/tmp/conntrack-tests"

echo "=============================================="
echo "Conntrack Test Runner"
echo "=============================================="
echo ""

for HOST in "${HOSTS[@]}"; do
    echo "-------------------------------------------"
    echo "Testing host: $HOST"
    echo "-------------------------------------------"
    
    # Get host info
    echo "Getting host info..."
    HOSTNAME=$(ssh -o ConnectTimeout=5 root@$HOST "hostname 2>/dev/null || echo 'unknown'" 2>/dev/null || echo "FAILED")
    KERNEL=$(ssh -o ConnectTimeout=5 root@$HOST "uname -r 2>/dev/null || echo 'unknown'" 2>/dev/null || echo "FAILED")
    OS=$(ssh -o ConnectTimeout=5 root@$HOST "cat /etc/os-release | grep PRETTY_NAME | cut -d= -f2 | tr -d '\"' 2>/dev/null || echo 'unknown'" 2>/dev/null || echo "FAILED")
    
    echo "  Hostname: $HOSTNAME"
    echo "  Kernel:   $KERNEL"
    echo "  OS:       $OS"
    echo ""
    
    # Check if Go is installed
    echo "Checking Go installation..."
    GO_VERSION=$(ssh root@$HOST "go version 2>/dev/null || echo 'not installed'" 2>/dev/null)
    if [[ "$GO_VERSION" == *"not installed"* ]]; then
        echo "  ⚠ Go not installed on $HOST"
        echo ""
        continue
    fi
    echo "  Go: $GO_VERSION"
    echo ""
    
    # Create remote directory
    echo "Creating remote directory..."
    ssh root@$HOST "mkdir -p $REMOTE_DIR"
    
    # Copy project files
    echo "Copying project files..."
    scp -r "$PROJECT_ROOT/go.mod" root@$HOST:$REMOTE_DIR/ 2>/dev/null || true
    scp -r "$PROJECT_ROOT/go.sum" root@$HOST:$REMOTE_DIR/ 2>/dev/null || true
    scp -r "$PROJECT_ROOT/internal/conntrack" root@$HOST:$REMOTE_DIR/internal/ 2>/dev/null || true
    scp -r "$PROJECT_ROOT/internal/config" root@$HOST:$REMOTE_DIR/internal/ 2>/dev/null || true
    scp -r "$PROJECT_ROOT/pkg/embedded" root@$HOST:$REMOTE_DIR/pkg/ 2>/dev/null || true
    scp -r "$PROJECT_ROOT/tests/conntrack" root@$HOST:$REMOTE_DIR/tests/ 2>/dev/null || true
    scp -r "$PROJECT_ROOT/bpf" root@$HOST:$REMOTE_DIR/ 2>/dev/null || true
    
    # Run unit tests
    echo ""
    echo "Running unit tests..."
    ssh root@$HOST "cd $REMOTE_DIR && go test -v ./internal/conntrack/... -run '^Test[^I]' 2>&1 | tee /tmp/unit_tests.log" || echo "Unit tests failed"
    
    # Run integration tests
    echo ""
    echo "Running integration tests..."
    ssh root@$HOST "cd $REMOTE_DIR && go test -v ./tests/conntrack/integration/... 2>&1 | tee /tmp/integration_tests.log" || echo "Integration tests failed"
    
    # Download results
    echo ""
    echo "Downloading results..."
    scp root@$HOST:/tmp/unit_tests.log "$SCRIPT_DIR/results_${HOST}_unit.log" 2>/dev/null || true
    scp root@$HOST:/tmp/integration_tests.log "$SCRIPT_DIR/results_${HOST}_integration.log" 2>/dev/null || true
    
    # Cleanup
    echo "Cleaning up..."
    ssh root@$HOST "rm -rf $REMOTE_DIR" 2>/dev/null || true
    
    echo ""
    echo "Results saved to:"
    echo "  - $SCRIPT_DIR/results_${HOST}_unit.log"
    echo "  - $SCRIPT_DIR/results_${HOST}_integration.log"
    echo ""
done

echo "=============================================="
echo "Test run complete!"
echo "=============================================="
