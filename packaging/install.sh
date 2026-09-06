#!/usr/bin/env bash
#
# Network Monitor Installation Script
# Installs netmon as a systemd service
#
# Usage: sudo ./install.sh [VERSION]
#

set -euo pipefail

# Configuration
NETMON_USER="${NETMON_USER:-root}"
NETMON_GROUP="${NETMON_GROUP:-root}"
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/netmon"
DATA_DIR="/var/lib/netmon"
LOG_DIR="/var/log/netmon"
SYSTEMD_DIR="/etc/systemd/system"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Logging functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[OK]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check if running as root
check_root() {
    if [[ $EUID -ne 0 ]]; then
        log_error "This script must be run as root (use sudo)"
        exit 1
    fi
}

# Detect system architecture
detect_arch() {
    local arch
    arch=$(uname -m)
    case $arch in
        x86_64)
            echo "amd64"
            ;;
        aarch64|arm64)
            log_error "ARM64 packages are not published or runtime-qualified"
            exit 1
            ;;
        *)
            log_error "Unsupported architecture: $arch"
            exit 1
            ;;
    esac
}

# Check system requirements
check_requirements() {
    log_info "Checking system requirements..."

    # Check for tracefs
    if ! mountpoint -q /sys/kernel/tracing 2>/dev/null; then
        log_warn "tracefs not mounted. Will attempt to mount it."
    else
        log_success "tracefs is mounted"
    fi

    # Check for systemd
    if ! command -v systemctl &>/dev/null; then
        log_error "systemd not found. This script requires systemd."
        exit 1
    fi
    log_success "systemd found"

    # Check for wget/curl
    if command -v wget &>/dev/null; then
        DOWNLOAD_CMD="wget -qO-"
    elif command -v curl &>/dev/null; then
        DOWNLOAD_CMD="curl -fsSL"
    else
        log_error "Neither wget nor curl found. Please install one."
        exit 1
    fi
    log_success "Download tool found: $(command -v wget 2>/dev/null || command -v curl)"
}

# Create directories
create_directories() {
    log_info "Creating directories..."

    mkdir -p "$INSTALL_DIR"
    mkdir -p "$CONFIG_DIR"
    mkdir -p "$DATA_DIR"
    mkdir -p "$LOG_DIR"

    chown -R "$NETMON_USER:$NETMON_GROUP" "$DATA_DIR"
    chown -R "$NETMON_USER:$NETMON_GROUP" "$LOG_DIR"

    log_success "Directories created"
}

# Mount tracefs if needed
mount_tracefs() {
    if ! mountpoint -q /sys/kernel/tracing 2>/dev/null; then
        log_info "Mounting tracefs..."
        mount -t tracefs none /sys/kernel/tracing
        log_success "tracefs mounted"
    fi
}

# Download and install binary
install_binary() {
    local version="${1:-latest}"
    local arch
    arch=$(detect_arch)
    local binary_name="netmon-linux-$arch"
    local download_url

    if [[ "$version" == "latest" ]]; then
        download_url="https://github.com/vponomarev/network-monitor/releases/latest/download/$binary_name"
    else
        download_url="https://github.com/vponomarev/network-monitor/releases/download/$version/$binary_name"
    fi

    log_info "Downloading netmon $version ($arch)..."

    local staged checksums expected actual
    staged=$(mktemp "$INSTALL_DIR/.netmon-download.XXXXXX")
    checksums=$(mktemp "$INSTALL_DIR/.netmon-checksums.XXXXXX")
    if ! $DOWNLOAD_CMD "$download_url" > "$staged" || ! $DOWNLOAD_CMD "${download_url%/*}/checksums.txt" > "$checksums"; then
        rm -f -- "$staged" "$checksums"
        log_error "Download failed; existing binary preserved"
        return 1
    fi
    expected=$(awk -v name="$binary_name" '$2 == name {print $1}' "$checksums")
    actual=$(sha256sum "$staged" | cut -d ' ' -f1)
    rm -f -- "$checksums"
    if [[ ! "$expected" =~ ^[0-9a-f]{64}$ || "$actual" != "$expected" ]]; then
        rm -f -- "$staged"; log_error "Checksum mismatch"; return 1
    fi
    chmod 0755 "$staged"
    if ! "$staged" --version >/dev/null 2>&1; then rm -f -- "$staged"; return 1; fi
    if [[ -f "$INSTALL_DIR/netmon" ]]; then cp -p "$INSTALL_DIR/netmon" "$INSTALL_DIR/netmon.previous"; fi
    mv -f -- "$staged" "$INSTALL_DIR/netmon"
    log_success "Verified binary installed atomically"

}

# Install local binary (from build)
install_local_binary() {
    local local_binary="${1:-./bin/netmon}"

    if [[ ! -f "$local_binary" ]]; then
        log_error "Local binary not found: $local_binary"
        exit 1
    fi

    log_info "Installing local binary..."
    local staged
    staged=$(mktemp "$INSTALL_DIR/.netmon-local.XXXXXX")
    install -m 0755 "$local_binary" "$staged"
    if ! "$staged" --version >/dev/null 2>&1; then rm -f -- "$staged"; return 1; fi
    if [[ -f "$INSTALL_DIR/netmon" ]]; then cp -p "$INSTALL_DIR/netmon" "$INSTALL_DIR/netmon.previous"; fi
    mv -f -- "$staged" "$INSTALL_DIR/netmon"
    log_success "Binary installed: $INSTALL_DIR/netmon"
}

# Install configuration files
install_config() {
    log_info "Installing configuration files..."

    # Install example config if not exists
    if [[ ! -f "$CONFIG_DIR/config.yaml" ]]; then
        cp configs/config.example.yaml "$CONFIG_DIR/config.yaml"
        log_success "Created default config: $CONFIG_DIR/config.yaml"
    else
        log_warn "Config already exists: $CONFIG_DIR/config.yaml"
    fi

    for name in locations roles; do
        if [[ ! -f "$CONFIG_DIR/$name.yaml" ]]; then
            install -m 0644 "configs/$name.example.yaml" "$CONFIG_DIR/$name.yaml"
        fi
    done

    # Install topology if not exists
    if [[ ! -f "$CONFIG_DIR/topology.yaml" ]]; then
        cp configs/topology.example.yaml "$CONFIG_DIR/topology.yaml" 2>/dev/null || true
        log_success "Created example topology: $CONFIG_DIR/topology.yaml"
    fi
}

# Install systemd service
install_systemd() {
    log_info "Installing systemd service..."

    # Copy service file
    cp packaging/netmon.service "$SYSTEMD_DIR/netmon.service"

    # Reload systemd
    systemctl daemon-reload

    # Enable service
    systemctl enable netmon.service

    log_success "Systemd service installed and enabled"
}

# Configure firewall (optional)
configure_firewall() {
    log_info "Default listener is loopback; configure authenticated remote access and firewall explicitly"
}

# Start service
start_service() {
    log_info "Starting netmon service..."

    systemctl restart netmon.service || true

    # Wait for service to start
    sleep 2

    if systemctl is-active --quiet netmon.service; then
        log_success "netmon service started"
    else
        log_error "Failed to start netmon service"
        if [[ -f "$INSTALL_DIR/netmon.previous" ]]; then
            mv -f "$INSTALL_DIR/netmon.previous" "$INSTALL_DIR/netmon"
            systemctl restart netmon.service || true
            log_warn "Restored previous binary"
        fi
        systemctl status netmon.service --no-pager || true
        exit 1
    fi
}

# Print status
print_status() {
    echo ""
    echo "=========================================="
    echo "  Network Monitor Installation Complete"
    echo "=========================================="
    echo ""
    echo "Service Status:"
    systemctl status netmon.service --no-pager -l
    echo ""
    echo "Configuration:"
    echo "  Binary:     $INSTALL_DIR/netmon"
    echo "  Config:     $CONFIG_DIR/config.yaml"
    echo "  Data:       $DATA_DIR"
    echo "  Logs:       $LOG_DIR"
    echo ""
    echo "Useful commands:"
    echo "  systemctl status netmon      # Check status"
    echo "  systemctl stop netmon        # Stop service"
    echo "  systemctl start netmon       # Start service"
    echo "  systemctl restart netmon     # Restart service"
    echo "  journalctl -u netmon -f      # View logs"
    echo ""
    echo "Metrics endpoint:"
    echo "  http://localhost:9876/metrics"
    echo ""
}

# Main
main() {
    echo "=========================================="
    echo "  Network Monitor Installer"
    echo "=========================================="
    echo ""

    check_root
    check_requirements
    create_directories
    mount_tracefs

    # Check if local binary exists
    if [[ -f "./bin/netmon" ]]; then
        log_info "Found local binary, installing from build..."
        install_local_binary "./bin/netmon"
    else
        log_info "Downloading latest release..."
        install_binary "${1:-latest}"
    fi

    install_config
    install_systemd
    configure_firewall
    start_service
    print_status
}

# Run main function
if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then main "$@"; fi
