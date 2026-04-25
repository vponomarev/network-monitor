# Development Guide

This guide covers developing Network Monitor on macOS for Linux targets.

## Platform Support

| Feature | macOS | Linux |
|---------|-------|-------|
| Build Go code | ✓ | ✓ |
| Run unit tests | ✓ | ✓ |
| Build eBPF | ✗ | ✓ |
| Run eBPF programs | ✗ | ✓ |
| Access trace_pipe | ✗ | ✓ |
| Integration tests | Limited | Full |

## Development Workflow on macOS

### 1. Daily Development

```bash
# Build for macOS (stub implementations)
make build

# Run unit tests (non-Linux specific)
make test

# Cross-compile for Linux
make build-linux
```

### 2. Testing Linux-Specific Code

#### Option A: Docker (Recommended)

```bash
# Run tests in Linux container
make test-linux

# Build Linux binaries with eBPF
make docker-build

# Interactive development shell
make docker-shell
```

#### Option B: Remote Linux VM

```bash
# Build for Linux
make build-linux

# Copy to VM
scp bin/* user@linux-vm:/usr/local/bin/

# SSH and test
ssh user@linux-vm
sudo /usr/local/bin/pktloss -i eth0
```

#### Option C: Vagrant

```bash
# Create Vagrantfile
cat > Vagrantfile << 'EOF'
Vagrant.configure("2") do |config|
  config.vm.box = "ubuntu/jammy64"
  config.vm.provision "shell", inline: <<-SHELL
    apt-get update
    apt-get install -y clang llvm libbpf-dev linux-headers-$(uname -r)
  SHELL
end
EOF

# Start VM
vagrant up

# SSH and test
vagrant ssh
```

### 3. eBPF Development

eBPF programs must be developed and tested on Linux:

```bash
# On macOS: Write/edit eBPF code
# bpf/conntrack.bpf.c

# Option 1: Use Docker
docker run --rm -it \
  -v $(pwd):/src \
  -w /src \
  ghcr.io/cilium/ebpf \
  make -C bpf

# Option 2: SSH to Linux VM
ssh user@linux-vm
cd /path/to/network-monitor
make build-ebpf
sudo ./bin/conntrack
```

## Build Tags

The code uses build tags for platform-specific implementations:

```go
//go:build linux
// +build linux

// Linux-specific code (eBPF, trace_pipe)
```

```go
//go:build !linux
// +build !linux

// Stub implementations for macOS/Windows
```

## Testing Strategy

### Unit Tests (macOS compatible)

```bash
# Run on macOS
go test ./internal/config/...
go test ./internal/metrics/...
```

### Integration Tests (Linux required)

```bash
# Run in Docker
docker run --rm \
  -v $(pwd):/src \
  -w /src \
  golang:1.21 \
  go test -v -tags=integration ./tests/integration/...
```

### E2E Tests (Linux with root)

```bash
# Run on Linux VM
ssh user@linux-vm
cd /path/to/network-monitor
sudo go test -v -tags=e2e ./tests/e2e/...
```

## CI/CD

GitHub Actions runs all tests on Linux:

- `.github/workflows/ci.yml` - Build and test on Ubuntu
- `.github/workflows/ebpf-build.yml` - eBPF compilation
- `.github/workflows/release.yml` - Multi-arch releases

## Debugging

### macOS

```bash
# Debug Go code
dlv debug ./cmd/netmon

# View logs
./bin/netmon --config configs/netmon.yaml.example
```

### Linux (via SSH/Docker)

```bash
# Debug with root access
sudo dlv debug ./cmd/pktloss

# View trace_pipe
sudo cat /sys/kernel/tracing/trace_pipe

# View eBPF events
sudo bpftool prog tracelog
```

## Recommended Setup

### For macOS Development

1. **Local**: Go IDE (GoLand/VSCode) for coding
2. **Testing**: Docker for Linux tests
3. **eBPF**: Linux VM or cloud instance
4. **CI**: GitHub Actions for final validation

### Docker Development Container

Create `.devcontainer/devcontainer.json`:

```json
{
  "name": "Network Monitor (Linux)",
  "image": "golang:1.21",
  "runArgs": [
    "--cap-add=SYS_PTRACE",
    "--security-opt", "seccomp=unconfined"
  ],
  "mounts": [
    "source=/sys/kernel/tracing,target=/sys/kernel/tracing,type=bind"
  ],
  "postCreateCommand": "apt-get update && apt-get install -y clang llvm libbpf-dev"
}
```

## Quick Reference

| Task | macOS Command | Linux Command |
|------|---------------|---------------|
| Build | `make build` | `make build` |
| Build eBPF | `make docker-build` | `make build-ebpf` |
| Test | `make test` | `make test` |
| Install | N/A | `sudo make install` |
| Run | `./bin/netmon` | `sudo ./bin/netmon` |

## Troubleshooting

### "eBPF compilation requires Linux"

Use Docker or cross-compile:
```bash
make docker-build
# or
make build-linux
```

### "trace_pipe not accessible"

This is expected on macOS. Test on Linux:
```bash
make test-linux
```

### "permission denied" on Linux

Run with sudo:
```bash
sudo ./bin/pktloss -i eth0
```
