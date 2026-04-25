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
            echo "arm64"
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
        DOWNLOAD_CMD="curl -sL"
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

    if $DOWNLOAD_CMD "$download_url" > "$INSTALL_DIR/netmon" 2>/dev/null; then
        chmod +x "$INSTALL_DIR/netmon"
        log_success "Binary installed: $INSTALL_DIR/netmon"
    else
        log_error "Failed to download binary from $download_url"
        exit 1
    fi
}

# Install local binary (from build)
install_local_binary() {
    local local_binary="${1:-./bin/netmon}"

    if [[ ! -f "$local_binary" ]]; then
        log_error "Local binary not found: $local_binary"
        exit 1
    fi

    log_info "Installing local binary..."
    cp "$local_binary" "$INSTALL_DIR/netmon"
    chmod +x "$INSTALL_DIR/netmon"
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
    log_info "Configuring firewall (optional)..."

    if command -v firewall-cmd &>/dev/null; then
        # firewalld
        firewall-cmd --permanent --add-port=9876/tcp 2>/dev/null || true
        firewall-cmd --reload 2>/dev/null || true
        log_success "firewalld configured (port 9876)"
    elif command -v ufw &>/dev/null; then
        # ufw
        ufw allow 9876/tcp 2>/dev/null || true
        log_success "ufw configured (port 9876)"
    else
        log_warn "No firewall manager found (firewalld/ufw). Configure manually if needed."
    fi
}

# Start service
start_service() {
    log_info "Starting netmon service..."

    systemctl start netmon.service

    # Wait for service to start
    sleep 2

    if systemctl is-active --quiet netmon.service; then
        log_success "netmon service started"
    else
        log_error "Failed to start netmon service"
        systemctl status netmon.service --no-pager
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
main "$@"
