#!/usr/bin/env bash
#
# Network Monitor Uninstallation Script
# Removes netmon systemd service and all files
#
# Usage: sudo ./uninstall.sh [--purge]
#

set -euo pipefail

# Configuration
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/netmon"
DATA_DIR="/var/lib/netmon"
LOG_DIR="/var/log/netmon"
SYSTEMD_DIR="/etc/systemd/system"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[OK]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

# Check if running as root
check_root() {
    if [[ $EUID -ne 0 ]]; then
        echo -e "${RED}[ERROR]${NC} This script must be run as root (use sudo)"
        exit 1
    fi
}

# Stop and disable service
stop_service() {
    log_info "Stopping netmon service..."

    if systemctl is-active --quiet netmon.service 2>/dev/null; then
        systemctl stop netmon.service
        log_success "Service stopped"
    else
        log_warn "Service was not running"
    fi

    systemctl disable netmon.service 2>/dev/null || true
    log_success "Service disabled"
}

# Remove systemd service
remove_systemd() {
    log_info "Removing systemd service..."

    rm -f "$SYSTEMD_DIR/netmon.service"
    systemctl daemon-reload

    log_success "Systemd service removed"
}

# Remove binary
remove_binary() {
    log_info "Removing binary..."

    if [[ -f "$INSTALL_DIR/netmon" ]]; then
        rm -f "$INSTALL_DIR/netmon"
        log_success "Binary removed: $INSTALL_DIR/netmon"
    else
        log_warn "Binary not found"
    fi
}

# Remove configuration
remove_config() {
    local purge="${1:-false}"

    if [[ "$purge" == "true" ]]; then
        log_info "Removing all configuration (purge mode)..."

        rm -rf "$CONFIG_DIR"
        log_success "Configuration directory removed: $CONFIG_DIR"
    else
        log_info "Preserving configuration (use --purge to remove)..."
        log_warn "Config kept at: $CONFIG_DIR"
    fi
}

# Remove data
remove_data() {
    local purge="${1:-false}"

    if [[ "$purge" == "true" ]]; then
        log_info "Removing all data (purge mode)..."

        rm -rf "$DATA_DIR"
        log_success "Data directory removed: $DATA_DIR"
    else
        log_info "Preserving data (use --purge to remove)..."
        log_warn "Data kept at: $DATA_DIR"
    fi
}

# Remove logs
remove_logs() {
    local purge="${1:-false}"

    if [[ "$purge" == "true" ]]; then
        log_info "Removing logs (purge mode)..."

        rm -rf "$LOG_DIR"
        log_success "Log directory removed: $LOG_DIR"
    else
        log_info "Preserving logs (use --purge to remove)..."
        log_warn "Logs kept at: $LOG_DIR"
    fi
}

# Print summary
print_summary() {
    local purge="${1:-false}"

    echo ""
    echo "=========================================="
    echo "  Network Monitor Uninstallation Complete"
    echo "=========================================="
    echo ""

    if [[ "$purge" == "true" ]]; then
        echo "All files have been removed."
    else
        echo "Preserved (use --purge to remove):"
        echo "  Config: $CONFIG_DIR"
        echo "  Data:   $DATA_DIR"
        echo "  Logs:   $LOG_DIR"
    fi

    echo ""
    echo "Removed:"
    echo "  Binary: $INSTALL_DIR/netmon"
    echo "  Service: systemd/netmon.service"
    echo ""
}

# Main
main() {
    local purge="false"

    if [[ "${1:-}" == "--purge" ]]; then
        purge="true"
    fi

    echo "=========================================="
    echo "  Network Monitor Uninstaller"
    echo "=========================================="
    echo ""

    check_root

    stop_service
    remove_systemd
    remove_binary
    remove_config "$purge"
    remove_data "$purge"
    remove_logs "$purge"
    print_summary "$purge"
}

# Run main function
main "$@"
