#!/usr/bin/env bash
# Build eBPF programs
# Usage: ./scripts/build-ebpf.sh

set -euo pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo -e "${GREEN}=== Building eBPF Programs ===${NC}"

# Check for clang
if ! command -v clang &> /dev/null; then
    echo -e "${RED}Error: clang is required for eBPF compilation${NC}"
    echo "Install with: sudo apt-get install clang llvm"
    exit 1
fi

# Check for bpftool
if ! command -v bpftool &> /dev/null; then
    echo -e "${YELLOW}Warning: bpftool not found, skeleton generation disabled${NC}"
fi

# Get architecture
ARCH=$(uname -m | sed 's/x86_64/x86/' | sed 's/aarch64/arm64/')
echo "Architecture: ${ARCH}"

# Build
cd "$(dirname "$0")/../bpf"

echo -e "\n${YELLOW}Compiling eBPF programs...${NC}"
make ARCH="${ARCH}"

# Verify output
echo -e "\n${YELLOW}Verifying eBPF objects...${NC}"
for obj in *.o; do
    if [ -f "$obj" ]; then
        echo "  $obj: $(file "$obj" | cut -d: -f2)"
    fi
done

echo -e "\n${GREEN}=== eBPF Build Complete ===${NC}"
