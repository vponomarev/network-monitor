# Installation Guide

This guide covers installing Network Monitor on Linux systems.

## Prerequisites

### System Requirements

- Linux kernel 5.8+ (for full eBPF support)
- Go 1.21+ (for building from source)
- Root/sudo access

### Build Dependencies

**Ubuntu/Debian:**
```bash
sudo apt-get update
sudo apt-get install -y \
    golang-go \
    clang \
    llvm \
    libbpf-dev \
    linux-headers-$(uname -r) \
    make \
    git
```

**RHEL/CentOS/Fedora:**
```bash
sudo yum install -y \
    golang \
    clang \
    llvm \
    libbpf-devel \
    kernel-devel \
    make \
    git
```

**Arch Linux:**
```bash
sudo pacman -S go clang llvm libbpf linux-headers make git
```

## Installation Methods

### Method 1: Pre-built Binaries (Recommended)

Download pre-built binaries from the [releases page](https://github.com/vponomarev/network-monitor/releases):

```bash
# Download latest release
wget https://github.com/vponomarev/network-monitor/releases/latest/download/netmon-linux-amd64
wget https://github.com/vponomarev/network-monitor/releases/latest/download/pktloss-linux-amd64
wget https://github.com/vponomarev/network-monitor/releases/latest/download/conntrack-linux-amd64

# Make executable
chmod +x netmon-linux-amd64 pktloss-linux-amd64 conntrack-linux-amd64

# Install to system
sudo mv netmon-linux-amd64 /usr/local/bin/netmon
sudo mv pktloss-linux-amd64 /usr/local/bin/pktloss
sudo mv conntrack-linux-amd64 /usr/local/bin/conntrack
```

### Method 2: Build from Source

```bash
# Clone repository
git clone https://github.com/vponomarev/network-monitor.git
cd network-monitor

# Download dependencies
make deps

# Build eBPF programs (requires clang)
make build-ebpf

# Build all binaries
make build

# Install to /usr/local/bin
sudo make install
```

### Method 3: Docker

```bash
# Build image
docker build -t network-monitor:latest .

# Run container (requires privileged mode for eBPF)
docker run --privileged --rm \
    -v /sys/kernel/tracing:/sys/kernel/tracing \
    -v /proc:/proc \
    network-monitor:latest
```

## Post-Installation

### Configure tracefs (for packet loss monitoring)

```bash
# Mount tracefs if not already mounted
sudo mount -t tracefs tracefs /sys/kernel/tracing

# Make permanent
echo "tracefs /sys/kernel/tracing tracefs defaults 0 0" | sudo tee -a /etc/fstab
```

### Configure eBPF (for connection tracking)

```bash
# Ensure BPF filesystem is mounted
sudo mount -t bpf bpf /sys/fs/bpf

# Make permanent
echo "bpf /sys/fs/bpf bpf defaults 0 0" | sudo tee -a /etc/fstab
```

### Create Configuration

```bash
# Create config directory
sudo mkdir -p /etc/netmon

# Copy example config
sudo cp configs/netmon.yaml.example /etc/netmon/config.yaml

# Edit configuration
sudo nano /etc/netmon/config.yaml
```

### Install Systemd Services (Optional)

```bash
# Copy service files
sudo cp configs/systemd/*.service /etc/systemd/system/

# Reload systemd
sudo systemctl daemon-reload

# Enable and start services
sudo systemctl enable netmon.service
sudo systemctl start netmon.service

# Check status
sudo systemctl status netmon.service
```

## Verification

### Check binaries are installed

```bash
netmon --version
pktloss --version
conntrack --version
```

### Test packet loss monitor

```bash
# Run briefly (requires root)
sudo timeout 5 pktloss --interface lo
```

### Test metrics endpoint

```bash
# Start netmon in background
sudo netmon &

# Check metrics
curl http://localhost:9090/metrics

# Stop netmon
sudo pkill netmon
```

## Troubleshooting

### "permission denied" errors

Ensure you're running with appropriate privileges:
```bash
sudo netmon
```

### "trace_pipe not found" errors

Mount tracefs:
```bash
sudo mount -t tracefs tracefs /sys/kernel/tracing
```

### eBPF program load failures

Check kernel version (5.8+ recommended):
```bash
uname -r
```

Verify BPF filesystem:
```bash
mount | grep bpf
```

### Missing kernel headers

Install kernel headers for your running kernel:
```bash
# Ubuntu/Debian
sudo apt-get install linux-headers-$(uname -r)

# RHEL/CentOS
sudo yum install kernel-devel-$(uname -r)
```

## Uninstallation

```bash
# Stop services
sudo systemctl stop netmon.service pktloss.service conntrack.service
sudo systemctl disable netmon.service pktloss.service conntrack.service

# Remove binaries
sudo rm /usr/local/bin/netmon /usr/local/bin/pktloss /usr/local/bin/conntrack

# Remove config
sudo rm -rf /etc/netmon

# Remove systemd services
sudo rm /etc/systemd/system/netmon.service
sudo rm /etc/systemd/system/pktloss.service
sudo rm /etc/systemd/system/conntrack.service

sudo systemctl daemon-reload
```
