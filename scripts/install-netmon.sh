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
    aarch64) log_error "Only Linux amd64 releases are qualified"; exit 1 ;;
    *) log_error "Unsupported architecture: $ARCH"; exit 1 ;;
esac

log_info "Installing netmon for Linux/$ARCH"

# Create directories
log_info "Creating directories..."
mkdir -p "$INSTALL_DIR" "$CONFIG_DIR"

# Resolve a single tag so binary and configuration always come from the same release.
if [[ "$NETMON_VERSION" == latest ]]; then
    resolved=$(curl -fsSL -o /dev/null -w '%{url_effective}' https://github.com/vponomarev/network-monitor/releases/latest)
    NETMON_VERSION=${resolved##*/}
fi
if [[ ! "$NETMON_VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+[-a-zA-Z0-9.]*$ ]]; then
    log_error "Invalid release tag"; exit 1
fi
base="https://github.com/vponomarev/network-monitor/releases/download/$NETMON_VERSION"
stage=$(mktemp -d "$INSTALL_DIR/.netmon-install.XXXXXX")
trap 'rm -rf -- "$stage"' EXIT
asset="netmon-$NETMON_VERSION-linux-$ARCH.tar.gz"
curl -fsSL "$base/$asset" -o "$stage/$asset"
curl -fsSL "$base/checksums.txt" -o "$stage/checksums.txt"
expected=$(awk -v name="$asset" '$2 == name {print $1}' "$stage/checksums.txt")
actual=$(sha256sum "$stage/$asset" | cut -d ' ' -f1)
[[ "$expected" =~ ^[0-9a-f]{64}$ && "$actual" == "$expected" ]] || { log_error "Checksum mismatch"; exit 1; }
# Extract only known regular release assets, never arbitrary archive paths.
prefix="netmon-$NETMON_VERSION-linux-$ARCH"
tar -xOf "$stage/$asset" "$prefix/netmon" > "$stage/netmon"
chmod 0755 "$stage/netmon"
"$stage/netmon" --version >/dev/null
for name in config locations roles topology; do
    tar -xOf "$stage/$asset" "$prefix/configs/$name.yaml" > "$stage/$name.yaml"
done
for name in config locations roles topology; do
    if [[ ! -e "$CONFIG_DIR/$name.yaml" ]]; then
        install -m 0644 "$stage/$name.yaml" "$CONFIG_DIR/$name.yaml"
    fi
done
if [[ -f "$INSTALL_DIR/$BIN_NAME" ]]; then cp -p "$INSTALL_DIR/$BIN_NAME" "$INSTALL_DIR/$BIN_NAME.previous"; fi
mv -f -- "$stage/netmon" "$INSTALL_DIR/$BIN_NAME"
ln -sf "$INSTALL_DIR/$BIN_NAME" /usr/local/bin/$BIN_NAME

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
