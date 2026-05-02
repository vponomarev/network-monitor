#!/usr/bin/env bash
# Conntrack Installation Script
# Automatically installs conntrack binary, eBPF program, and configuration

set -euo pipefail

CONNTRACK_VERSION="${CONNTRACK_VERSION:-latest}"
INSTALL_DIR="${INSTALL_DIR:-/opt/conntrack}"
CONFIG_DIR="${CONFIG_DIR:-/etc/netmon}"
EBPF_DIR="${EBPF_DIR:-/usr/share/netmon/bpf}"
BIN_NAME="conntrack"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check if running as root
if [[ $EUID -ne 0 ]]; then
    log_error "This script must be run as root (use sudo)"
    exit 1
fi

# Detect architecture
ARCH=$(uname -m)
case $ARCH in
    x86_64) ARCH="amd64" ;;
    aarch64) ARCH="arm64" ;;
    *) log_error "Unsupported architecture: $ARCH"; exit 1 ;;
esac

log_info "Installing conntrack for Linux/$ARCH"

# Check kernel version (eBPF requires 4.9+)
KERNEL_VERSION=$(uname -r)
log_info "Kernel version: $KERNEL_VERSION"

# Create directories
log_info "Creating directories..."
mkdir -p "$INSTALL_DIR" "$CONFIG_DIR" "$EBPF_DIR"

# Download binary
BINARY_URL="https://github.com/vponomarev/network-monitor/releases/${CONNTRACK_VERSION}/download/conntrack-linux-${ARCH}"
log_info "Downloading binary from $BINARY_URL"
curl -fsSL "$BINARY_URL" -o "$INSTALL_DIR/$BIN_NAME" || {
    log_error "Failed to download binary. Check if the release exists."
    exit 1
}
chmod +x "$INSTALL_DIR/$BIN_NAME"

# Create symlink
log_info "Creating symlink in /usr/local/bin"
ln -sf "$INSTALL_DIR/$BIN_NAME" /usr/local/bin/$BIN_NAME

# Download eBPF program
EBPF_URL="https://github.com/vponomarev/network-monitor/releases/${CONNTRACK_VERSION}/download/conntrack.bpf.o"
log_info "Downloading eBPF program from $EBPF_URL"
curl -fsSL "$EBPF_URL" -o "$EBPF_DIR/conntrack.bpf.o" || {
    log_error "Failed to download eBPF program. Check if the release exists."
    exit 1
}

# Download configuration files
log_info "Downloading configuration files..."

# Create conntrack-specific config
cat > "$CONFIG_DIR/conntrack.yaml" << 'EOF'
# Conntrack Configuration
# Connection tracking with eBPF

connections:
  enabled: true
  track_incoming: true
  track_outgoing: true
  track_closes: true
  filter_ports: []

logging:
  level: info
  format: json
  syslog:
    enabled: true
    tag: conntrack
    facility: LOCAL0
EOF

log_info "Configuration created: $CONFIG_DIR/conntrack.yaml"

# Verify eBPF program
log_info "Verifying eBPF program..."
if [[ -f "$EBPF_DIR/conntrack.bpf.o" ]]; then
    log_info "eBPF program installed: $EBPF_DIR/conntrack.bpf.o"
else
    log_error "eBPF program not found"
    exit 1
fi

# Verify installation
log_info "Verifying installation..."
if "$INSTALL_DIR/$BIN_NAME" --version >/dev/null 2>&1; then
    VERSION=$("$INSTALL_DIR/$BIN_NAME" --version)
    log_info "Conntrack installed successfully! Version: $VERSION"
else
    log_error "Installation verification failed"
    exit 1
fi

# Print next steps
echo ""
log_info "=========================================="
log_info "Conntrack has been installed successfully!"
log_info "=========================================="
echo ""
echo "Next steps:"
echo "  1. Edit configuration: $CONFIG_DIR/conntrack.yaml"
echo "  2. Start conntrack: sudo conntrack --config $CONFIG_DIR/conntrack.yaml"
echo "  3. Check API: curl http://localhost:9876/api/v1/conntrack/connections"
echo "  4. View metrics: curl http://localhost:9876/metrics | grep conntrack"
echo ""
echo "Note: conntrack requires eBPF support (Linux kernel 4.9+)"
echo ""
echo "Documentation: https://github.com/vponomarev/network-monitor/blob/main/INSTALL.md"
echo ""
