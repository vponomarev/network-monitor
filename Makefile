.PHONY: all build test clean lint help deps docker-build docker-run docker-stop docker-logs install uninstall

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test
GOMOD=$(GOCMD) mod
GOFMT=gofmt
GOVET=$(GOCMD) vet

# Binary name
BINARY=netmon
BUILD_DIR=bin

# Docker parameters
DOCKER_IMAGE=netmon
DOCKER_TAG=latest

# Version
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME=$(shell date -u '+%Y-%m-%d_%H:%M:%S')
GIT_COMMIT=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

LDFLAGS=-ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME) -X main.GitCommit=$(GIT_COMMIT)"

all: build

## build: Build the binary
build:
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY) ./cmd/netmon

## test: Run all tests
test:
	$(GOTEST) -v -race ./...

## test-coverage: Run tests with coverage report
test-coverage:
	$(GOTEST) -v -race -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

## clean: Clean build artifacts
clean:
	rm -rf $(BUILD_DIR)
	rm -f coverage.out coverage.html

## lint: Run linters
lint:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed. Install with: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
	fi

## fmt: Format code
fmt:
	$(GOFMT) -s -w .

## vet: Run go vet
vet:
	$(GOVET) ./...

## deps: Download and tidy dependencies
deps:
	$(GOMOD) download
	$(GOMOD) tidy

## run: Run the application (requires sudo for trace_pipe)
run: build
	sudo ./$(BUILD_DIR)/$(BINARY)

## docker-build: Build Docker image
docker-build:
	docker build -t $(DOCKER_IMAGE):$(DOCKER_TAG) .

## docker-run: Run Docker container (requires sudo)
docker-run:
	docker-compose up -d

## docker-stop: Stop Docker container
docker-stop:
	docker-compose down

## docker-logs: Show Docker container logs
docker-logs:
	docker-compose logs -f netmon

## docker-clean: Remove Docker image and container
docker-clean:
	docker-compose down -v
	docker rmi $(DOCKER_IMAGE):$(DOCKER_TAG) 2>/dev/null || true

## install: Install as systemd service (Linux only)
install: build
	sudo ./packaging/install.sh local

## uninstall: Remove systemd service
uninstall:
	sudo ./packaging/uninstall.sh

## package: Create release package
package: build
	@echo "Creating release package..."
	@mkdir -p dist
	cp bin/netmon dist/
	cp configs/*.yaml dist/
	cp packaging/*.sh dist/
	cp README.md dist/
	@echo "Package created in dist/"

## help: Show this help message
help:
	@echo "Network Monitor - Build System"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@sed -n 's/^##//p' $(MAKEFILE_LIST) | column -t -s ':' | sed -e 's/^/ /'
