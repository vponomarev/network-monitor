# Systemd Deployment Guide

## Overview

Production-ready systemd service for deploying Network Monitor on Linux servers.

---

## Quick Install

### One-Line Installation

```bash
# Download and run installer
curl -sL https://github.com/vponomarev/network-monitor/releases/latest/download/install.sh | sudo bash
```

### Manual Installation

```bash
# Download installer
wget https://github.com/vponomarev/network-monitor/releases/latest/download/install.sh
chmod +x install.sh

# Run installation
sudo ./install.sh
```

---

## Installation Script

### Features

- ✅ Automatic binary download (latest release)
- ✅ Creates directories and users
- ✅ Mounts tracefs if needed
- ✅ Installs systemd service
- ✅ Configures firewall (firewalld/ufw)
- ✅ Starts service automatically

### Usage

```bash
# Install latest version
sudo ./packaging/install.sh

# Install specific version
sudo ./packaging/install.sh v1.0.0

# Install from local build
make build
sudo ./packaging/install.sh local
```

### Uninstallation

```bash
# Remove service (keep config/data)
sudo ./packaging/uninstall.sh

# Complete purge (remove everything)
sudo ./packaging/uninstall.sh --purge
```

---

## Manual Installation

### Step 1: Build Binary

```bash
# Build for Linux
CGO_ENABLED=0 GOOS=linux go build -o netmon ./cmd/netmon

# Or use make
make build
```

### Step 2: Copy Files

```bash
# Copy binary
sudo cp bin/netmon /usr/local/bin/netmon
sudo chmod +x /usr/local/bin/netmon

# Create directories
sudo mkdir -p /etc/netmon
sudo mkdir -p /var/lib/netmon
sudo mkdir -p /var/log/netmon

# Copy config
sudo cp configs/config.example.yaml /etc/netmon/config.yaml
sudo cp configs/topology.example.yaml /etc/netmon/topology.yaml
```

### Step 3: Mount tracefs

```bash
# Mount tracefs
sudo mount -t tracefs none /sys/kernel/tracing

# Make permanent
echo 'tracefs /sys/kernel/tracing tracefs defaults 0 0' | sudo tee -a /etc/fstab
```

### Step 4: Install Systemd Service

```bash
# Copy service file
sudo cp packaging/netmon.service /etc/systemd/system/netmon.service

# Reload systemd
sudo systemctl daemon-reload

# Enable service
sudo systemctl enable netmon

# Start service
sudo systemctl start netmon
```

### Step 5: Verify

```bash
# Check status
sudo systemctl status netmon

# View logs
sudo journalctl -u netmon -f

# Check metrics
curl http://localhost:9876/metrics
```

---

## Configuration

### Edit Config

```bash
sudo vim /etc/netmon/config.yaml
```

### Reload After Config Changes

```bash
# Send SIGHUP (graceful reload)
sudo systemctl reload netmon

# Or restart
sudo systemctl restart netmon
```

### Configuration Files

| File | Path | Purpose |
|------|------|---------|
| Main config | `/etc/netmon/config.yaml` | Application settings |
| Topology | `/etc/netmon/topology.yaml` | Network topology |
| Locations | `/etc/netmon/locations.yaml` | Location matchers |
| Roles | `/etc/netmon/roles.yaml` | Role matchers |

---

## Service Management

### Basic Commands

```bash
# Status
sudo systemctl status netmon

# Start
sudo systemctl start netmon

# Stop
sudo systemctl stop netmon

# Restart
sudo systemctl restart netmon

# Reload (SIGHUP)
sudo systemctl reload netmon

# Enable at boot
sudo systemctl enable netmon

# Disable at boot
sudo systemctl disable netmon
```

### View Logs

```bash
# Follow logs
sudo journalctl -u netmon -f

# Last 100 lines
sudo journalctl -u netmon -n 100

# Since boot
sudo journalctl -u netmon --boot

# With priority filter
sudo journalctl -u netmon -p err

# Time range
sudo journalctl -u netmon --since "2024-01-01 00:00:00" --until "2024-01-01 23:59:59"
```

### Resource Usage

```bash
# CPU/Memory
systemctl show netmon --property=MemoryCurrent,CPUUsageNS

# File descriptors
ls -l /proc/$(systemctl show netmon -p MainPID --value)/fd | wc -l
```

---

## Security

### Capabilities

The service requires:

| Capability | Purpose |
|------------|---------|
| `CAP_SYS_ADMIN` | Access trace_pipe |
| `CAP_NET_RAW` | Raw sockets for traceroute |

### Hardening Options

Edit `/etc/systemd/system/netmon.service`:

```ini
[Service]
# Read-only root filesystem (advanced)
ReadOnlyPaths=/

# Private tmp
PrivateTmp=true

# No new privileges
NoNewPrivileges=true

# Protect kernel
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
```

After changes:
```bash
sudo systemctl daemon-reload
sudo systemctl restart netmon
```

### Firewall Configuration

```bash
# firewalld
sudo firewall-cmd --permanent --add-port=9876/tcp
sudo firewall-cmd --reload

# ufw
sudo ufw allow 9876/tcp

# iptables
sudo iptables -A INPUT -p tcp --dport 9876 -j ACCEPT
```

---

## Troubleshooting

### Service Won't Start

```bash
# Check status
sudo systemctl status netmon

# Check logs
sudo journalctl -u netmon -n 50

# Test binary manually
sudo /usr/local/bin/netmon --config /etc/netmon/config.yaml
```

### Common Issues

#### tracefs Not Mounted

**Error:** `trace_pipe not found`

**Solution:**
```bash
sudo mount -t tracefs none /sys/kernel/tracing
echo 'tracefs /sys/kernel/tracing tracefs defaults 0 0' | sudo tee -a /etc/fstab
```

#### Permission Denied

**Error:** `permission denied`

**Solution:**
```bash
# Check capabilities in service file
cat /etc/systemd/system/netmon.service | grep -i capability

# Restart service
sudo systemctl daemon-reload
sudo systemctl restart netmon
```

#### High Memory Usage

**Solution:**
```bash
# Reduce metrics TTL
# Edit /etc/netmon/config.yaml
global:
  ttl_hours: 1  # Reduce from 3

# Restart
sudo systemctl restart netmon
```

#### No Metrics

**Solution:**
```bash
# Check trace_pipe
sudo cat /sys/kernel/tracing/trace_pipe

# Enable tcp_retransmit_skb event
echo 1 | sudo tee /sys/kernel/tracing/events/tcp/tcp_retransmit_skb/enable

# Restart service
sudo systemctl restart netmon
```

---

## Monitoring

### Health Check

```bash
# HTTP health endpoint
curl http://localhost:9876/health

# Systemd health
systemctl is-active netmon
```

### Prometheus Integration

Add to `prometheus.yml`:

```yaml
scrape_configs:
  - job_name: 'netmon'
    static_configs:
      - targets: ['localhost:9876']
    metrics_path: /metrics
    scrape_interval: 30s
```

### Grafana Dashboard

Import dashboard from `grafana/dashboard.json`:

1. Open Grafana
2. Dashboards → Import
3. Upload JSON file
4. Select Prometheus datasource

---

## Performance Tuning

### Adjust Trace Events

```bash
# Only enable specific events
echo 0 | sudo tee /sys/kernel/tracing/events/tcp/enable
echo 1 | sudo tee /sys/kernel/tracing/events/tcp/tcp_retransmit_skb/enable

# Make permanent (systemd service)
sudo systemctl edit netmon
```

### Resource Limits

Edit `/etc/systemd/system/netmon.service`:

```ini
[Service]
# CPU limit
CPUQuota=100%

# Memory limit
MemoryLimit=512M

# File descriptors
LimitNOFILE=65536
```

### Log Rotation

Create `/etc/logrotate.d/netmon`:

```
/var/log/netmon/*.log {
    daily
    rotate 7
    compress
    delaycompress
    missingok
    notifempty
    create 0640 root root
    postrotate
        systemctl reload netmon
    endscript
}
```

---

## Update Procedure

### Automatic (Installer)

```bash
# Stop service
sudo systemctl stop netmon

# Run installer
sudo ./packaging/install.sh latest

# Start service
sudo systemctl start netmon
```

### Manual

```bash
# Download new binary
wget https://github.com/vponomarev/network-monitor/releases/latest/download/netmon-linux-amd64

# Backup old binary
sudo cp /usr/local/bin/netmon /usr/local/bin/netmon.bak

# Install new binary
sudo cp netmon-linux-amd64 /usr/local/bin/netmon
sudo chmod +x /usr/local/bin/netmon

# Reload (graceful)
sudo systemctl reload netmon

# Verify
sudo systemctl status netmon

# Remove backup
sudo rm /usr/local/bin/netmon.bak
```

---

## Files Summary

| File | Purpose |
|------|---------|
| `packaging/netmon.service` | Systemd unit file |
| `packaging/install.sh` | Installation script |
| `packaging/uninstall.sh` | Uninstallation script |
| `/etc/netmon/config.yaml` | Main configuration |
| `/etc/netmon/topology.yaml` | Network topology |
| `/var/lib/netmon/` | Data directory |
| `/var/log/netmon/` | Log directory |

---

## Best Practices

1. **Always use SIGHUP for config reload** - Graceful, no connection loss
2. **Monitor disk space** - Logs can grow large
3. **Set up log rotation** - Prevent disk exhaustion
4. **Use configuration management** - Ansible/Puppet/Chef for scale
5. **Test in staging first** - Validate before production
6. **Backup configs** - Version control your configuration
7. **Monitor the monitor** - Alert on netmon service status

---

## Support

- **Documentation:** `docs/` directory
- **Issues:** GitHub Issues
- **Logs:** `journalctl -u netmon -f`

---

*Production-ready deployment with systemd!*
