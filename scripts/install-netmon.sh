#!/usr/bin/env bash
# Netmon Installation Script
# Automatically installs netmon binary and configuration

set -euo pipefail

NETMON_VERSION="${NETMON_VERSION:-latest}"
INSTALL_DIR="${INSTALL_DIR:-/opt/netmon}"
CONFIG_DIR="${CONFIG_DIR:-/etc/netmon}"
BIN_NAME="netmon"

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

log_info "Installing netmon for Linux/$ARCH"

# Create directories
log_info "Creating directories..."
mkdir -p "$INSTALL_DIR" "$CONFIG_DIR"

# Download binary
BINARY_URL="https://github.com/vponomarev/network-monitor/releases/${NETMON_VERSION}/download/netmon-linux-${ARCH}"
log_info "Downloading binary from $BINARY_URL"
curl -fsSL "$BINARY_URL" -o "$INSTALL_DIR/$BIN_NAME" || {
    log_error "Failed to download binary. Check if the release exists."
    exit 1
}
chmod +x "$INSTALL_DIR/$BIN_NAME"

# Create symlink
log_info "Creating symlink in /usr/local/bin"
ln -sf "$INSTALL_DIR/$BIN_NAME" /usr/local/bin/$BIN_NAME

# Download configuration files
log_info "Downloading configuration files..."
curl -fsSL "https://raw.githubusercontent.com/vponomarev/network-monitor/main/configs/config.example.yaml" -o "$CONFIG_DIR/config.yaml" || {
    log_warn "Failed to download config.example.yaml"
}

curl -fsSL "https://raw.githubusercontent.com/vponomarev/network-monitor/main/configs/locations.example.yaml" -o "$CONFIG_DIR/locations.yaml" || {
    log_warn "Failed to download locations.example.yaml"
}

curl -fsSL "https://raw.githubusercontent.com/vponomarev/network-monitor/main/configs/roles.example.yaml" -o "$CONFIG_DIR/roles.yaml" || {
    log_warn "Failed to download roles.example.yaml"
}

# Mount tracefs if not already mounted
if ! mountpoint -q /sys/kernel/tracing 2>/dev/null; then
    log_info "Mounting tracefs..."
    mount -t tracefs none /sys/kernel/tracing || {
        log_warn "Failed to mount tracefs. You may need to mount it manually:"
        log_warn "  mount -t tracefs none /sys/kernel/tracing"
    }
fi

# Verify installation
log_info "Verifying installation..."
if "$INSTALL_DIR/$BIN_NAME" --version >/dev/null 2>&1; then
    VERSION=$("$INSTALL_DIR/$BIN_NAME" --version)
    log_info "Netmon installed successfully! Version: $VERSION"
else
    log_error "Installation verification failed"
    exit 1
fi

# Print next steps
echo ""
log_info "=========================================="
log_info "Netmon has been installed successfully!"
log_info "=========================================="
echo ""
echo "Next steps:"
echo "  1. Edit configuration: $CONFIG_DIR/config.yaml"
echo "  2. Start netmon: sudo netmon --config $CONFIG_DIR/config.yaml"
echo "  3. Check health: curl http://localhost:9876/health"
echo "  4. View metrics: curl http://localhost:9876/metrics"
echo ""
echo "Documentation: https://github.com/vponomarev/network-monitor/blob/main/INSTALL.md"
echo ""
