#!/usr/bin/env bash
#
# Script for building and deploying conntrack on remote hosts
# Usage: ./scripts/build-deploy-conntrack.sh [host]
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
REMOTE_DIR="/tmp/conntrack-build"
LOCAL_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# Check dependencies on remote host
check_deps() {
    local host="$1"
    print_info "Checking dependencies on $host..."
    
    ssh "$REMOTE_USER@$host" << 'EOF'
# Check required packages
which clang >/dev/null 2>&1 || { echo "clang not found"; exit 1; }
which llvm-strip >/dev/null 2>&1 || { echo "llvm-strip not found"; exit 1; }
ls /usr/include/bpf/bpf_helpers.h >/dev/null 2>&1 || { echo "libbpf-dev not found"; exit 1; }
ls /sys/kernel/btf/vmlinux >/dev/null 2>&1 || { echo "BTF not available"; exit 1; }

echo "All dependencies OK"
EOF
}

# Install dependencies on remote host
install_deps() {
    local host="$1"
    print_info "Installing dependencies on $host..."
    
    ssh "$REMOTE_USER@$host" << 'EOF'
# Install required packages (Debian/Ubuntu)
apt-get update
apt-get install -y clang llvm libbpf-dev linux-headers-$(uname -r)

# Verify installation
which clang && which llvm-strip && ls /usr/include/bpf/bpf_helpers.h
EOF
    
    print_success "Dependencies installed on $host"
}

# Copy source files to remote host
copy_source() {
    local host="$1"
    print_info "Copying source files to $host..."
    
    ssh "$REMOTE_USER@$host" "mkdir -p $REMOTE_DIR/bpf $REMOTE_DIR/cmd/conntrack $REMOTE_DIR/internal/conntrack"
    
    # Copy bpf sources
    scp "$LOCAL_DIR/bpf"/*.c "$LOCAL_DIR/bpf"/*.h "$LOCAL_DIR/bpf/Makefile" "$REMOTE_USER@$host:$REMOTE_DIR/bpf/"
    
    # Copy Go sources
    scp "$LOCAL_DIR/cmd/conntrack"/*.go "$REMOTE_USER@$host:$REMOTE_DIR/cmd/conntrack/" 2>/dev/null || true
    scp "$LOCAL_DIR/internal/conntrack"/*.go "$REMOTE_USER@$host:$REMOTE_DIR/internal/conntrack/" 2>/dev/null || true
    scp "$LOCAL_DIR/go.mod" "$LOCAL_DIR/go.sum" "$REMOTE_USER@$host:$REMOTE_DIR/"
    
    print_success "Source files copied to $host"
}

# Build on remote host
build_remote() {
    local host="$1"
    print_info "Building on $host..."
    
    ssh "$REMOTE_USER@$host" << 'EOF'
cd /tmp/conntrack-build

# Build eBPF programs
echo "Building eBPF programs..."
make -C bpf all

# Build Go binary
echo "Building Go binary..."
go build -o conntrack ./cmd/conntrack

# Verify build
ls -la bpf/*.o conntrack
echo "Build completed successfully"
EOF
    
    print_success "Build completed on $host"
}

# Deploy to remote host
deploy() {
    local host="$1"
    print_info "Deploying to $host..."
    
    ssh "$REMOTE_USER@$host" << 'EOF'
# Create installation directories
mkdir -p /usr/local/bin /usr/share/conntrack/bpf

# Copy binaries
cp /tmp/conntrack-build/conntrack /usr/local/bin/
cp /tmp/conntrack-build/bpf/*.o /usr/share/conntrack/bpf/
chmod +x /usr/local/bin/conntrack

echo "Deployment completed"
ls -la /usr/local/bin/conntrack /usr/share/conntrack/bpf/
EOF
    
    print_success "Deployed to $host"
}

# Run tests on remote host
run_tests() {
    local host="$1"
    print_info "Running tests on $host..."
    
    ssh "$REMOTE_USER@$host" << 'EOF'
cd /tmp/conntrack-build

# Run integration tests
echo "Running conntrack tests..."
go test -v ./internal/conntrack/... -run "TestConntrack" 2>&1 | head -50
EOF
}

# Main
main() {
    local host="${1:-}"
    
    if [ -z "$host" ]; then
        print_error "Usage: $0 <host>"
        print_info "Example: $0 192.168.5.99"
        exit 1
    fi
    
    print_info "Building and deploying conntrack on $host"
    print_info "=========================================="
    
    # Check if dependencies are installed
    if ! check_deps "$host" 2>/dev/null; then
        print_info "Dependencies missing, installing..."
        install_deps "$host"
    else
        print_success "Dependencies already installed"
    fi
    
    # Copy source files
    copy_source "$host"
    
    # Build
    build_remote "$host"
    
    # Deploy
    deploy "$host"
    
    # Optional: run tests
    # run_tests "$host"
    
    print_info ""
    print_success "Build and deployment completed on $host!"
    print_info ""
    print_info "To start conntrack:"
    print_info "  ssh $REMOTE_USER@$host"
    print_info "  sudo conntrack --config /path/to/config.yaml"
}

main "$@"
