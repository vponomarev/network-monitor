#!/usr/bin/env bash
# Build script for Network Monitor
# Usage: ./scripts/build.sh [options]

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Default values
BUILD_EBPF=true
BUILD_TESTS=false
OUTPUT_DIR="bin"
LDFLAGS=""

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --no-ebpf)
            BUILD_EBPF=false
            shift
            ;;
        --with-tests)
            BUILD_TESTS=true
            shift
            ;;
        --output)
            OUTPUT_DIR="$2"
            shift 2
            ;;
        --release)
            LDFLAGS="-s -w"
            shift
            ;;
        -h|--help)
            echo "Usage: $0 [options]"
            echo ""
            echo "Options:"
            echo "  --no-ebpf     Skip eBPF compilation"
            echo "  --with-tests  Build test binaries"
            echo "  --output DIR  Output directory (default: bin)"
            echo "  --release     Build release binaries (strip symbols)"
            echo "  -h, --help    Show this help"
            exit 0
            ;;
        *)
            echo -e "${RED}Unknown option: $1${NC}"
            exit 1
            ;;
    esac
done

echo -e "${GREEN}=== Network Monitor Build ===${NC}"

# Check Go installation
if ! command -v go &> /dev/null; then
    echo -e "${RED}Error: Go is not installed${NC}"
    exit 1
fi

GO_VERSION=$(go version | cut -d' ' -f3)
echo "Go version: ${GO_VERSION}"

# Create output directory
mkdir -p "${OUTPUT_DIR}"

# Download dependencies
echo -e "\n${YELLOW}Downloading dependencies...${NC}"
go mod download

# Build eBPF programs
if [ "${BUILD_EBPF}" = true ]; then
    echo -e "\n${YELLOW}Building eBPF programs...${NC}"
    if command -v clang &> /dev/null; then
        make -C bpf || echo -e "${YELLOW}Warning: eBPF build failed${NC}"
    else
        echo -e "${YELLOW}Warning: clang not found, skipping eBPF build${NC}"
    fi
fi

# Get version info
VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME=$(date -u '+%Y-%m-%d_%H:%M:%S')
GIT_COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")

LDFLAGS="${LDFLAGS} -X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME} -X main.GitCommit=${GIT_COMMIT}"

# Build binaries
echo -e "\n${YELLOW}Building binaries...${NC}"

echo "  Building netmon..."
go build -ldflags "${LDFLAGS}" -o "${OUTPUT_DIR}/netmon" ./cmd/netmon

echo "  Building pktloss..."
go build -ldflags "${LDFLAGS}" -o "${OUTPUT_DIR}/pktloss" ./cmd/pktloss

echo "  Building conntrack..."
go build -ldflags "${LDFLAGS}" -o "${OUTPUT_DIR}/conntrack" ./cmd/conntrack

# Build test binary if requested
if [ "${BUILD_TESTS}" = true ]; then
    echo -e "\n${YELLOW}Building test binary...${NC}"
    go test -c -o "${OUTPUT_DIR}/tests.test" ./...
fi

echo -e "\n${GREEN}=== Build Complete ===${NC}"
echo "Binaries are in: ${OUTPUT_DIR}/"
ls -la "${OUTPUT_DIR}/"

echo -e "\n${GREEN}Version: ${VERSION}${NC}"
echo -e "${GREEN}Build time: ${BUILD_TIME}${NC}"
echo -e "${GREEN}Git commit: ${GIT_COMMIT}${NC}"
