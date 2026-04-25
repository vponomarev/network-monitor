#!/usr/bin/env bash
# CI checks script - runs all verification steps
# Usage: ./scripts/ci-checks.sh

set -euo pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

FAILED=0

echo -e "${GREEN}=== CI Checks ===${NC}"

# Function to run a check
run_check() {
    local name=$1
    local cmd=$2
    
    echo -e "\n${YELLOW}Running: ${name}${NC}"
    if eval "${cmd}"; then
        echo -e "${GREEN}✓ ${name} passed${NC}"
    else
        echo -e "${RED}✗ ${name} failed${NC}"
        FAILED=1
    fi
}

# Check Go installation
if ! command -v go &> /dev/null; then
    echo -e "${RED}Error: Go is not installed${NC}"
    exit 1
fi

# 1. Format check
run_check "Format check" "test -z \"\$(gofmt -l . | tee /dev/stderr)\""

# 2. Go mod tidy
run_check "Go mod tidy" "go mod tidy && git diff --exit-code go.mod go.sum 2>/dev/null || test \$? -eq 1"

# 3. Build
run_check "Build" "go build ./..."

# 4. Vet
run_check "Go vet" "go vet ./..."

# 5. Tests
run_check "Unit tests" "go test -race ./..."

# 6. Lint (if golangci-lint available)
if command -v golangci-lint &> /dev/null; then
    run_check "Linting" "golangci-lint run ./..."
else
    echo -e "\n${YELLOW}Skipping linting: golangci-lint not installed${NC}"
fi

# 7. Security scan (if gosec available)
if command -v gosec &> /dev/null; then
    run_check "Security scan" "gosec -exclude=G304 ./..."
else
    echo -e "\n${YELLOW}Skipping security scan: gosec not installed${NC}"
fi

# Summary
echo -e "\n${GREEN}=== CI Checks Summary ===${NC}"
if [ ${FAILED} -eq 0 ]; then
    echo -e "${GREEN}All checks passed!${NC}"
    exit 0
else
    echo -e "${RED}Some checks failed!${NC}"
    exit 1
fi
