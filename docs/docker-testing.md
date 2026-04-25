# Docker-based Testing for Linux-Specific Features

This directory contains Docker configurations for testing Linux-specific features from macOS.

## Quick Start

### Run Tests in Docker

```bash
# Simple test run
make test-linux

# With verbose output
docker run --rm -v $(pwd):/src -w /src golang:1.21 go test -v ./...
```

### Build Linux Binaries

```bash
# Build with eBPF support
make docker-build

# Or manually
docker run --rm -v $(pwd):/src -w /src golang:1.21 make build
```

## Docker Images

### Basic Go Testing

```bash
docker run --rm \
  -v $(pwd):/src \
  -w /src \
  golang:1.21 \
  go test -race ./...
```

### With eBPF Support

```bash
docker run --rm \
  -v $(pwd):/src \
  -w /src \
  --cap-add SYS_PTRACE \
  golang:1.21 \
  sh -c "apt-get update && apt-get install -y clang llvm libbpf-dev && make build"
```

### Full Linux Environment

For testing with actual kernel features, use a privileged container:

```bash
docker run --rm -it \
  -v $(pwd):/src \
  -w /src \
  --privileged \
  --pid=host \
  -v /sys/kernel/tracing:/sys/kernel/tracing \
  ubuntu:22.04 \
  bash
```

## Docker Compose

For complex testing scenarios:

```yaml
# docker-compose.test.yml
version: '3.8'
services:
  test:
    image: golang:1.21
    volumes:
      - .:/src
    working_dir: /src
    command: go test -v ./...
  
  ebpf-test:
    image: ubuntu:22.04
    privileged: true
    volumes:
      - .:/src
      - /sys/kernel/tracing:/sys/kernel/tracing
    working_dir: /src
    command: |
      apt-get update
      apt-get install -y golang clang llvm libbpf-dev
      make build-ebpf
      make test
```

## Development Shell

Start an interactive Docker shell:

```bash
make docker-shell
```

Or manually:

```bash
docker run --rm -it \
  -v $(pwd):/src \
  -w /src \
  golang:1.21 \
  bash
```

## GitHub Actions

The CI uses similar Docker-based testing:

```yaml
- name: Run tests
  run: |
    docker run --rm \
      -v $(pwd):/src \
      -w /src \
      golang:1.21 \
      go test -v -race ./...
```
