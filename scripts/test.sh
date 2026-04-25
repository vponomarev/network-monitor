#!/usr/bin/env bash
# Run tests for Network Monitor
# Usage: ./scripts/test.sh [options]

set -euo pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# Default values
COVERAGE=false
INTEGRATION=false
E2E=false
VERBOSE=false
RACE=true

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --coverage)
            COVERAGE=true
            shift
            ;;
        --integration)
            INTEGRATION=true
            shift
            ;;
        --e2e)
            E2E=true
            shift
            ;;
        --verbose|-v)
            VERBOSE=true
            shift
            ;;
        --no-race)
            RACE=false
            shift
            ;;
        -h|--help)
            echo "Usage: $0 [options]"
            echo ""
            echo "Options:"
            echo "  --coverage     Generate coverage report"
            echo "  --integration  Run integration tests (requires root)"
            echo "  --e2e          Run end-to-end tests (requires root and built binaries)"
            echo "  --verbose, -v  Verbose output"
            echo "  --no-race      Disable race detector"
            echo "  -h, --help     Show this help"
            exit 0
            ;;
        *)
            echo -e "${RED}Unknown option: $1${NC}"
            exit 1
            ;;
    esac
done

echo -e "${GREEN}=== Network Monitor Tests ===${NC}"

# Build test arguments
TEST_ARGS=()
if [ "${VERBOSE}" = true ]; then
    TEST_ARGS+=("-v")
fi
if [ "${RACE}" = true ]; then
    TEST_ARGS+=("-race")
fi

# Unit tests
echo -e "\n${YELLOW}Running unit tests...${NC}"
if [ "${COVERAGE}" = true ]; then
    go test "${TEST_ARGS[@]}" -coverprofile=coverage.out ./...
    echo -e "\n${GREEN}Coverage report:${NC}"
    go tool cover -func=coverage.out | tail -1
else
    go test "${TEST_ARGS[@]}" ./...
fi

# Integration tests
if [ "${INTEGRATION}" = true ]; then
    echo -e "\n${YELLOW}Running integration tests...${NC}"
    if [ "$(id -u)" -ne 0 ]; then
        echo -e "${RED}Warning: Integration tests require root privileges${NC}"
    fi
    go test "${TEST_ARGS[@]}" -tags=integration ./tests/integration/...
fi

# E2E tests
if [ "${E2E}" = true ]; then
    echo -e "\n${YELLOW}Running e2e tests...${NC}"
    if [ "$(id -u)" -ne 0 ]; then
        echo -e "${RED}Warning: E2E tests require root privileges${NC}"
    fi
    go test "${TEST_ARGS[@]}" -tags=e2e ./tests/e2e/...
fi

echo -e "\n${GREEN}=== All Tests Passed ===${NC}"
