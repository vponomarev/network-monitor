# Network Monitor - Build System
# Supports building both netmon and conntrack applications
#
# Usage: make [target]
# Examples:
#   make build          - Build both applications
#   make build-netmon   - Build only netmon
#   make build-conntrack - Build only conntrack
#   make all            - Build everything (default)

.PHONY: all build build-netmon build-conntrack build-conntrack-embedded test clean lint help deps \
        docker-build docker-build-netmon docker-build-conntrack \
        docker-run docker-stop docker-logs docker-clean \
        ebpf-build ebpf-clean prepare-embedded install uninstall package release

# =============================================================================
# Go parameters
# =============================================================================
GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test
GOMOD=$(GOCMD) mod
GOFMT=gofmt
GOVET=$(GOCMD) vet

# Build directory
BUILD_DIR=bin

# Version info
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME=$(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
GIT_COMMIT=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

LDFLAGS=-ldflags "-w -s -X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME) -X main.GitCommit=$(GIT_COMMIT)"

# Docker parameters
DOCKER_IMAGE=ghcr.io/vponomarev/network-monitor
DOCKER_TAG=latest

# =============================================================================
# Default target
# =============================================================================
all: build

# =============================================================================
# Build targets
# =============================================================================

## build: Build both netmon and conntrack binaries
build: build-netmon build-conntrack
	@echo "✓ Built both applications"

## build-netmon: Build netmon binary
build-netmon:
	@mkdir -p $(BUILD_DIR)
	@echo "Building netmon..."
	$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/netmon ./cmd/netmon
	@echo "✓ Built $(BUILD_DIR)/netmon"

## build-conntrack: Build conntrack binary (Linux only)
build-conntrack: ebpf-build
	@mkdir -p $(BUILD_DIR)
	@echo "Building conntrack..."
	$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/conntrack ./cmd/conntrack
	@echo "✓ Built $(BUILD_DIR)/conntrack"

## build-all: Build all binaries for current platform
build-all: build

# =============================================================================
# eBPF targets
# =============================================================================

## ebpf-build: Build eBPF programs
ebpf-build:
	@echo "Building eBPF programs..."
	@mkdir -p $(BUILD_DIR)/bpf
	$(MAKE) -C bpf all
	@cp bpf/*.o $(BUILD_DIR)/bpf/ 2>/dev/null || true
	@echo "✓ Built eBPF programs"

## ebpf-clean: Clean eBPF build artifacts
ebpf-clean:
	@echo "Cleaning eBPF artifacts..."
	$(MAKE) -C bpf clean
	rm -rf $(BUILD_DIR)/bpf

# =============================================================================
# Embedded resources targets
# =============================================================================

## prepare-embedded: Подготовить embedded ресурсы
prepare-embedded:
	@echo "Preparing embedded resources..."
	@mkdir -p pkg/embedded/bpf pkg/embedded/configs pkg/embedded/systemd
	@cp bpf/conntrack.bpf.o pkg/embedded/bpf/
	@cp configs/config.example.yaml pkg/embedded/configs/
	@cp packaging/systemd/conntrack.service pkg/embedded/systemd/ 2>/dev/null || true
	@echo "✓ Embedded resources prepared"

## build-conntrack-embedded: Сборка conntrack с embedded ресурсами
build-conntrack-embedded: ebpf-build prepare-embedded
	@mkdir -p $(BUILD_DIR)
	@echo "Building conntrack with embedded resources..."
	$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/conntrack ./cmd/conntrack
	@echo "✓ Built $(BUILD_DIR)/conntrack (single binary)"
	@ls -lh $(BUILD_DIR)/conntrack

## release-linux-amd64: Сборка релиза для linux/amd64
release-linux-amd64:
	@echo "Building release for linux/amd64..."
	GOOS=linux GOARCH=amd64 $(MAKE) build-conntrack-embedded
	@cp $(BUILD_DIR)/conntrack dist/conntrack-linux-amd64

## release: Создать все релиз артефакты
release: clean
	@echo "Creating release artifacts..."
	@mkdir -p dist
	$(MAKE) release-linux-amd64
	@cd dist && sha256sum conntrack-* > SHA256SUMS
	@echo "✓ Release artifacts created in dist/"
	@ls -lh dist/

# =============================================================================
# Test targets
# =============================================================================

## test: Run all tests
test:
	$(GOTEST) -v -race ./...

## test-coverage: Run tests with coverage report
test-coverage:
	$(GOTEST) -v -race -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "✓ Coverage report generated: coverage.html"

## test-integration: Run integration tests (requires root)
test-integration:
	@echo "Running integration tests (requires root)..."
	sudo $(GOTEST) -v ./tests/integration/...

## test-conntrack: Run conntrack connection tests (requires root)
test-conntrack:
	@echo "Running conntrack connection tests (requires root)..."
	sudo $(GOTEST) -v ./tests/integration/... -run "TestConntrack"

## test-conntrack-outgoing: Test outgoing connections (requires root)
test-conntrack-outgoing:
	@echo "Testing outgoing connections..."
	sudo $(GOTEST) -v ./tests/integration/... -run "TestConntrack_OutgoingConnections"

## test-conntrack-incoming: Test incoming connections (requires root)
test-conntrack-incoming:
	@echo "Testing incoming connections..."
	sudo $(GOTEST) -v ./tests/integration/... -run "TestConntrack_IncomingConnections"

## test-conntrack-handshake: Test TCP handshake (requires root)
test-conntrack-handshake:
	@echo "Testing TCP handshake..."
	sudo $(GOTEST) -v ./tests/integration/... -run "TestConntrack_TCPhandshake"

## test-e2e: Run end-to-end tests (requires root)
test-e2e:
	@echo "Running e2e tests (requires root)..."
	sudo $(GOTEST) -v ./tests/e2e/...

## test-remote: Run tests on remote hosts
test-remote:
	@echo "Running tests on remote hosts..."
	./scripts/run-remote-tests.sh

# =============================================================================
# Code quality targets
# =============================================================================

## lint: Run linters
lint:
	@echo "Running linters..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed. Install with:"; \
		echo "  go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
	fi

## fmt: Format code
fmt:
	@echo "Formatting code..."
	$(GOFMT) -s -w .

## vet: Run go vet
vet:
	@echo "Running go vet..."
	$(GOVET) ./...

## check: Run all checks (lint, vet, test)
check: lint vet test

# =============================================================================
# Dependency targets
# =============================================================================

## deps: Download and tidy dependencies
deps:
	@echo "Downloading dependencies..."
	$(GOMOD) download
	$(GOMOD) tidy

## deps-upgrade: Upgrade all dependencies
deps-upgrade:
	@echo "Upgrading dependencies..."
	$(GOMOD) get -u ./...
	$(GOMOD) tidy

# =============================================================================
# Docker targets
# =============================================================================

## docker-build: Build all Docker images
docker-build: docker-build-netmon docker-build-conntrack
	@echo "✓ Built all Docker images"

## docker-build-netmon: Build netmon Docker image
docker-build-netmon:
	@echo "Building netmon Docker image..."
	docker build --target netmon -t $(DOCKER_IMAGE)/netmon:$(DOCKER_TAG) .

## docker-build-conntrack: Build conntrack Docker image
docker-build-conntrack:
	@echo "Building conntrack Docker image..."
	docker build --target conntrack -t $(DOCKER_IMAGE)/conntrack:$(DOCKER_TAG) .

## docker-build-combined: Build combined Docker image
docker-build-combined:
	@echo "Building combined Docker image..."
	docker build --target combined -t $(DOCKER_IMAGE):$(DOCKER_TAG) .

## docker-run: Start Docker Compose services
docker-run:
	@echo "Starting Docker Compose services..."
	docker-compose up -d

## docker-run-monitoring: Start with Prometheus and Grafana
docker-run-monitoring:
	@echo "Starting Docker Compose with monitoring stack..."
	docker-compose --profile monitoring up -d

## docker-stop: Stop Docker Compose services
docker-stop:
	@echo "Stopping Docker Compose services..."
	docker-compose down

## docker-logs: Show Docker Compose logs
docker-logs:
	docker-compose logs -f

## docker-clean: Remove Docker images and volumes
docker-clean:
	@echo "Cleaning Docker artifacts..."
	docker-compose down -v
	docker rmi $(DOCKER_IMAGE)/netmon:$(DOCKER_TAG) 2>/dev/null || true
	docker rmi $(DOCKER_IMAGE)/conntrack:$(DOCKER_TAG) 2>/dev/null || true
	docker rmi $(DOCKER_IMAGE):$(DOCKER_TAG) 2>/dev/null || true

# =============================================================================
# Installation targets
# =============================================================================

## install: Install both applications locally
install: build
	@echo "Installing applications..."
	sudo mkdir -p /usr/local/bin
	sudo cp $(BUILD_DIR)/netmon /usr/local/bin/
	sudo cp $(BUILD_DIR)/conntrack /usr/local/bin/
	sudo chmod +x /usr/local/bin/netmon /usr/local/bin/conntrack
	@echo "✓ Installed to /usr/local/bin/"

## install-netmon: Install netmon locally
install-netmon: build-netmon
	@echo "Installing netmon..."
	sudo mkdir -p /usr/local/bin
	sudo cp $(BUILD_DIR)/netmon /usr/local/bin/
	sudo chmod +x /usr/local/bin/netmon
	@echo "✓ Installed netmon to /usr/local/bin/"

## install-conntrack: Install conntrack locally
install-conntrack: build-conntrack
	@echo "Installing conntrack..."
	sudo mkdir -p /usr/local/bin /usr/share/conntrack/bpf
	sudo cp $(BUILD_DIR)/conntrack /usr/local/bin/
	sudo cp $(BUILD_DIR)/bpf/*.o /usr/share/conntrack/bpf/
	sudo chmod +x /usr/local/bin/conntrack
	@echo "✓ Installed conntrack to /usr/local/bin/"

## uninstall: Remove installed applications
uninstall:
	@echo "Uninstalling applications..."
	sudo rm -f /usr/local/bin/netmon /usr/local/bin/conntrack
	sudo rm -rf /usr/share/conntrack
	@echo "✓ Uninstalled"

# =============================================================================
# Package targets
# =============================================================================

## package: Create release package
package: build ebpf-build
	@echo "Creating release package..."
	@mkdir -p dist
	cp $(BUILD_DIR)/netmon dist/
	cp $(BUILD_DIR)/conntrack dist/
	cp -r $(BUILD_DIR)/bpf dist/
	cp configs/*.yaml dist/
	cp README.md dist/
	cp LICENSE dist/ 2>/dev/null || true
	@echo "✓ Package created in dist/"

## package-tar: Create tarball for release
package-tar: package
	@echo "Creating tarball..."
	tar -czvf network-monitor-$(VERSION).tar.gz -C dist .
	@echo "✓ Created network-monitor-$(VERSION).tar.gz"

# =============================================================================
# Clean targets
# =============================================================================

## clean: Clean all build artifacts
clean: ebpf-clean
	@echo "Cleaning build artifacts..."
	rm -rf $(BUILD_DIR)
	rm -f coverage.out coverage.html
	rm -rf dist/
	@echo "✓ Cleaned"

## distclean: Clean everything including downloaded dependencies
distclean: clean
	@echo "Cleaning all artifacts including dependencies..."
	rm -rf vendor/
	@echo "✓ Cleaned all"

# =============================================================================
# Run targets
# =============================================================================

## run-netmon: Run netmon (requires sudo)
run-netmon: build-netmon
	@echo "Starting netmon (requires sudo)..."
	sudo $(BUILD_DIR)/netmon

## run-conntrack: Run conntrack (requires sudo)
run-conntrack: build-conntrack
	@echo "Starting conntrack (requires sudo)..."
	sudo $(BUILD_DIR)/conntrack

# =============================================================================
# Help target
# =============================================================================

## help: Show this help message
help:
	@echo "Network Monitor - Build System"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Build Targets:"
	@sed -n 's/^## \([a-z-]*\):/\1/p' $(MAKEFILE_LIST) | grep -E "^(build|ebpf)" | column -t -s ':' | sed 's/^/  /'
	@echo ""
	@echo "Test Targets:"
	@sed -n 's/^## \(test[a-z-]*\):/\1/p' $(MAKEFILE_LIST) | column -t -s ':' | sed 's/^/  /'
	@echo ""
	@echo "Code Quality:"
	@sed -n 's/^## \(lint\|fmt\|vet\|check\):/\1/p' $(MAKEFILE_LIST) | column -t -s ':' | sed 's/^/  /'
	@echo ""
	@echo "Docker Targets:"
	@sed -n 's/^## \(docker[a-z-]*\):/\1/p' $(MAKEFILE_LIST) | column -t -s ':' | sed 's/^/  /'
	@echo ""
	@echo "Install Targets:"
	@sed -n 's/^## \(install[a-z-]*\|uninstall\):/\1/p' $(MAKEFILE_LIST) | column -t -s ':' | sed 's/^/  /'
	@echo ""
	@echo "Other Targets:"
	@sed -n 's/^## \(deps\|clean\|package\|help\|run\):/\1/p' $(MAKEFILE_LIST) | column -t -s ':' | sed 's/^/  /'
	@echo ""
	@echo "Variables:"
	@echo "  VERSION=$(VERSION)"
	@echo "  BUILD_TIME=$(BUILD_TIME)"
	@echo "  GIT_COMMIT=$(GIT_COMMIT)"
