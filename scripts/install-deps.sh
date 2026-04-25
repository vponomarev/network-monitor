#!/usr/bin/env bash
# Install development dependencies
# Usage: ./scripts/install-deps.sh

set -euo pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo -e "${GREEN}=== Installing Development Dependencies ===${NC}"

# Detect OS
OS=$(uname -s | tr '[:upper:]' '[:lower:]')

case "${OS}" in
    linux*)
        # Detect package manager
        if command -v apt-get &> /dev/null; then
            echo -e "${YELLOW}Detected Debian/Ubuntu${NC}"
            sudo apt-get update
            sudo apt-get install -y \
                clang \
                llvm \
                libbpf-dev \
                linux-headers-$(uname -r) \
                bpftool \
                make \
                git
        elif command -v yum &> /dev/null; then
            echo -e "${YELLOW}Detected RHEL/CentOS${NC}"
            sudo yum install -y \
                clang \
                llvm \
                libbpf-devel \
                kernel-devel \
                bpftool \
                make \
                git
        elif command -v dnf &> /dev/null; then
            echo -e "${YELLOW}Detected Fedora${NC}"
            sudo dnf install -y \
                clang \
                llvm \
                libbpf-devel \
                kernel-devel \
                bpftool \
                make \
                git
        elif command -v pacman &> /dev/null; then
            echo -e "${YELLOW}Detected Arch Linux${NC}"
            sudo pacman -S --noconfirm \
                clang \
                llvm \
                libbpf \
                linux-headers \
                make \
                git
        else
            echo -e "${RED}Unsupported package manager${NC}"
            exit 1
        fi
        ;;
    darwin*)
        echo -e "${YELLOW}Detected macOS${NC}"
        if command -v brew &> /dev/null; then
            brew install llvm libbpf
        else
            echo -e "${RED}Homebrew not found. Please install: https://brew.sh${NC}"
            exit 1
        fi
        ;;
    *)
        echo -e "${RED}Unsupported OS: ${OS}${NC}"
        exit 1
        ;;
esac

# Install Go tools
echo -e "\n${YELLOW}Installing Go tools...${NC}"
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
go install github.com/securego/gosec/v2/cmd/gosec@latest
go install golang.org/x/vuln/cmd/govulncheck@latest

echo -e "\n${GREEN}=== Dependencies Installed ===${NC}"
echo -e "${GREEN}Note: eBPF features require Linux kernel 5.8+${NC}"
