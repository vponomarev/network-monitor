#!/usr/bin/env bash
#
# Script for running conntrack integration tests on remote hosts
# Usage: ./scripts/run-remote-tests.sh [host1] [host2] ...
#
# Example:
#   ./scripts/run-remote-tests.sh 192.168.5.99 192.168.5.214
#

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Default hosts
DEFAULT_HOSTS=("192.168.5.99" "192.168.5.214")
REMOTE_USER="root"
REMOTE_DIR="/tmp/network-monitor-tests"
LOCAL_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# Print colored message
print_msg() {
    local color="$1"
    local msg="$2"
    echo -e "${color}${msg}${NC}"
}

print_info() {
    print_msg "$BLUE" "[INFO] $1"
}

print_success() {
    print_msg "$GREEN" "[SUCCESS] $1"
}

print_warning() {
    print_msg "$YELLOW" "[WARNING] $1"
}

print_error() {
    print_msg "$RED" "[ERROR] $1"
}

# Check if host is reachable
check_host() {
    local host="$1"
    print_info "Checking connectivity to $host..."
    if ssh -o ConnectTimeout=5 -o BatchMode=yes "$REMOTE_USER@$host" "echo 'Connection successful'" >/dev/null 2>&1; then
        print_success "Host $host is reachable"
        return 0
    else
        print_error "Cannot connect to $host"
        return 1
    fi
}

# Get kernel version from remote host
get_kernel_version() {
    local host="$1"
    ssh "$REMOTE_USER@$host" "uname -r" 2>/dev/null || echo "unknown"
}

# Get OS info from remote host
get_os_info() {
    local host="$1"
    ssh "$REMOTE_USER@$host" "cat /etc/os-release 2>/dev/null | grep -E '^(PRETTY_NAME|ID)=' | head -2" 2>/dev/null || echo "unknown"
}

# Copy files to remote host
copy_to_remote() {
    local host="$1"
    print_info "Copying test files to $host..."
    
    # Create remote directory
    ssh "$REMOTE_USER@$host" "mkdir -p $REMOTE_DIR" 2>/dev/null
    
    # Copy test files
    rsync -avz -e "ssh -o StrictHostKeyChecking=no" \
        "$LOCAL_DIR/tests/integration/" \
        "$REMOTE_USER@$host:$REMOTE_DIR/tests/" 2>/dev/null || {
        # Fallback to scp if rsync not available
        scp -r "$LOCAL_DIR/tests/integration/" "$REMOTE_USER@$host:$REMOTE_DIR/tests/" 2>/dev/null
    }
    
    # Copy go.mod and go.sum
    scp "$LOCAL_DIR/go.mod" "$LOCAL_DIR/go.sum" "$REMOTE_USER@$host:$REMOTE_DIR/" 2>/dev/null
    
    # Copy internal packages needed for tests
    ssh "$REMOTE_USER@$host" "mkdir -p $REMOTE_DIR/internal/conntrack $REMOTE_DIR/internal/config" 2>/dev/null
    scp "$LOCAL_DIR/internal/conntrack"/*.go "$REMOTE_USER@$host:$REMOTE_DIR/internal/conntrack/" 2>/dev/null || true
    scp "$LOCAL_DIR/internal/config"/*.go "$REMOTE_USER@$host:$REMOTE_DIR/internal/config/" 2>/dev/null || true
    
    print_success "Files copied to $host"
}

# Run tests on remote host
run_tests_on_remote() {
    local host="$1"
    local kernel_version
    local os_info
    
    kernel_version=$(get_kernel_version "$host")
    os_info=$(get_os_info "$host")
    
    print_info "=========================================="
    print_info "Running tests on $host"
    print_info "Kernel: $kernel_version"
    print_info "OS: $os_info"
    print_info "=========================================="
    
    # Create test runner script
    cat > /tmp/remote_test_runner.sh << 'EOF'
#!/bin/bash
set -euo pipefail

cd /tmp/network-monitor-tests

# Download Go dependencies if needed
if [ ! -d "vendor" ]; then
    echo "Downloading Go dependencies..."
    go mod download 2>/dev/null || true
fi

# Run integration tests
echo "Running conntrack connection tests..."
cd tests/integration

# Run specific tests with verbose output
sudo go test -v -timeout 5m \
    -run "TestConntrack" \
    . 2>&1 | tee /tmp/test_results.log

# Check test results
if [ ${PIPESTATUS[0]} -eq 0 ]; then
    echo "TESTS_PASSED"
else
    echo "TESTS_FAILED"
    exit 1
fi
EOF
    
    scp /tmp/remote_test_runner.sh "$REMOTE_USER@$host:/tmp/run_tests.sh" 2>/dev/null
    ssh "$REMOTE_USER@$host" "chmod +x /tmp/run_tests.sh && /tmp/run_tests.sh" 2>&1
    
    local exit_code=$?
    
    # Fetch results
    print_info "Fetching test results from $host..."
    ssh "$REMOTE_USER@$host" "cat /tmp/test_results.log 2>/dev/null" > "$LOCAL_DIR/test_results_${host//./_}.log" 2>/dev/null || true
    
    if [ $exit_code -eq 0 ]; then
        print_success "Tests PASSED on $host"
    else
        print_error "Tests FAILED on $host"
    fi
    
    return $exit_code
}

# Cleanup remote files
cleanup_remote() {
    local host="$1"
    print_info "Cleaning up remote files on $host..."
    ssh "$REMOTE_USER@$host" "rm -rf $REMOTE_DIR /tmp/run_tests.sh /tmp/test_results.log" 2>/dev/null || true
    print_success "Cleanup completed on $host"
}

# Main function
main() {
    local hosts=("$@")
    
    if [ ${#hosts[@]} -eq 0 ]; then
        hosts=("${DEFAULT_HOSTS[@]}")
        print_info "No hosts specified, using defaults: ${hosts[*]}"
    fi
    
    print_info "Network Monitor - Remote Test Runner"
    print_info "======================================"
    print_info "Local directory: $LOCAL_DIR"
    print_info "Remote user: $REMOTE_USER"
    print_info "Remote directory: $REMOTE_DIR"
    print_info "Target hosts: ${hosts[*]}"
    print_info ""
    
    local passed=0
    local failed=0
    local skipped=0
    
    for host in "${hosts[@]}"; do
        print_info ""
        print_info "Processing host: $host"
        print_info "------------------------------------------"
        
        # Check connectivity
        if ! check_host "$host"; then
            print_warning "Skipping $host - not reachable"
            ((skipped++))
            continue
        fi
        
        # Copy files
        copy_to_remote "$host"
        
        # Run tests
        if run_tests_on_remote "$host"; then
            ((passed++))
        else
            ((failed++))
        fi
        
        # Cleanup (optional - comment out to keep files for debugging)
        # cleanup_remote "$host"
        
        print_info ""
    done
    
    # Summary
    print_info ""
    print_info "=========================================="
    print_info "TEST SUMMARY"
    print_info "=========================================="
    print_success "Passed: $passed"
    if [ $failed -gt 0 ]; then
        print_error "Failed: $failed"
    else
        print_info "Failed: $failed"
    fi
    if [ $skipped -gt 0 ]; then
        print_warning "Skipped: $skipped"
    else
        print_info "Skipped: $skipped"
    fi
    print_info "=========================================="
    
    # Exit with error if any tests failed
    if [ $failed -gt 0 ]; then
        exit 1
    fi
    
    exit 0
}

# Run main function
main "$@"
