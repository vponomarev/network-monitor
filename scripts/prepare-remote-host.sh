#!/usr/bin/env bash
#
# Script for preparing remote hosts for testing
# Installs Go and copies test files
#
# Usage: ./scripts/prepare-remote-host.sh [host]
#

set -euo pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

print_msg() {
    local color="$1"
    local msg="$2"
    echo -e "${color}${msg}${NC}"
}

print_info() { print_msg "$BLUE" "[INFO] $1"; }
print_success() { print_msg "$GREEN" "[SUCCESS] $1"; }
print_error() { print_msg "$RED" "[ERROR] $1"; }

REMOTE_USER="root"
REMOTE_DIR="/tmp/network-monitor-tests"
GO_VERSION="1.21.0"

# Check if Go is installed
check_go() {
    local host="$1"
    if ssh "$REMOTE_USER@$host" "which go >/dev/null 2>&1 && go version >/dev/null 2>&1"; then
        return 0
    fi
    return 1
}

# Install Go on remote host
install_go() {
    local host="$1"
    print_info "Installing Go $GO_VERSION on $host..."
    
    ssh "$REMOTE_USER@$host" << 'EOF'
# Download and install Go
cd /tmp
wget -q https://go.dev/dl/go1.21.0.linux-amd64.tar.gz
rm -rf /usr/local/go
tar -C /usr/local -xzf go1.21.0.linux-amd64.tar.gz
rm -f go1.21.0.linux-amd64.tar.gz

# Add to PATH
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
export PATH=$PATH:/usr/local/go/bin

# Verify
go version
EOF
    
    print_success "Go installed on $host"
}

# Copy test files
copy_files() {
    local host="$1"
    local local_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
    
    print_info "Copying test files to $host..."
    
    ssh "$REMOTE_USER@$host" "mkdir -p $REMOTE_DIR"
    
    # Copy test files
    rsync -avz -e "ssh -o StrictHostKeyChecking=no" \
        "$local_dir/tests/integration/" \
        "$REMOTE_USER@$host:$REMOTE_DIR/tests/" 2>/dev/null || \
    scp -r "$local_dir/tests/integration/" "$REMOTE_USER@$host:$REMOTE_DIR/tests/"
    
    # Copy go.mod and go.sum
    scp "$local_dir/go.mod" "$local_dir/go.sum" "$REMOTE_USER@$host:$REMOTE_DIR/"
    
    # Copy internal packages
    ssh "$REMOTE_USER@$host" "mkdir -p $REMOTE_DIR/internal/conntrack $REMOTE_DIR/internal/config"
    scp "$local_dir/internal/conntrack"/*.go "$REMOTE_USER@$host:$REMOTE_DIR/internal/conntrack/" 2>/dev/null || true
    scp "$local_dir/internal/config"/*.go "$REMOTE_USER@$host:$REMOTE_DIR/internal/config/" 2>/dev/null || true
    
    print_success "Files copied to $host"
}

# Check system info
check_system() {
    local host="$1"
    print_info "System info for $host:"
    ssh "$REMOTE_USER@$host" "uname -a"
    ssh "$REMOTE_USER@$host" "cat /etc/os-release | grep PRETTY_NAME"
    ssh "$REMOTE_USER@$host" "ls -la /sys/kernel/btf/vmlinux" 2>/dev/null || echo "BTF not available"
}

# Main
main() {
    local host="${1:-}"
    
    if [ -z "$host" ]; then
        print_error "Usage: $0 <host>"
        exit 1
    fi
    
    print_info "Preparing host: $host"
    print_info "======================================"
    
    # Check system
    check_system "$host"
    
    # Check/install Go
    if check_go "$host"; then
        print_success "Go is already installed on $host"
        ssh "$REMOTE_USER@$host" "go version"
    else
        print_info "Go not found on $host, installing..."
        install_go "$host"
    fi
    
    # Copy files
    copy_files "$host"
    
    print_info ""
    print_success "Host $host is ready for testing!"
    print_info ""
    print_info "To run tests:"
    print_info "  ssh $REMOTE_USER@$host"
    print_info "  cd $REMOTE_DIR/tests/integration"
    print_info "  sudo go test -v -run \"TestConntrack\" ."
}

main "$@"
