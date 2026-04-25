# Deployment Comparison

## Overview

Network Monitor supports multiple deployment methods. Choose based on your environment and requirements.

---

## Quick Comparison

| Method | Best For | Complexity | Security | Scalability |
|--------|----------|------------|----------|-------------|
| **Systemd** | Single server, VMs | ⭐ | ⭐⭐⭐ | ⭐ |
| **Docker** | Development, testing | ⭐⭐ | ⭐⭐ | ⭐⭐ |
| **Kubernetes** | Production, clusters | ⭐⭐⭐ | ⭐⭐⭐ | ⭐⭐⭐⭐⭐ |
| **Binary** | Simple deployments | ⭐ | ⭐⭐ | ⭐ |

---

## Method 1: Systemd Service (Recommended for Production)

### Pros
- ✅ Native Linux integration
- ✅ Automatic restart on failure
- ✅ Log integration (journalctl)
- ✅ Graceful reload (SIGHUP)
- ✅ Resource limits
- ✅ Boot startup

### Cons
- ❌ Linux only
- ❌ Manual scaling
- ❌ No container isolation

### Requirements
```bash
# Capabilities
CAP_SYS_ADMIN    # trace_pipe access
CAP_NET_RAW      # raw sockets
```

### Installation
```bash
# One-line
curl -sL https://github.com/vponomarev/network-monitor/releases/latest/download/install.sh | sudo bash

# Or manual
make build
sudo make install
```

### Management
```bash
systemctl status netmon
journalctl -u netmon -f
systemctl reload netmon  # Graceful reload
```

### Best For
- Production Linux servers
- VM deployments
- Single-server monitoring
- Environments without container orchestration

---

## Method 2: Docker Container

### Pros
- ✅ Easy deployment
- ✅ Version control via images
- ✅ Isolation from host
- ✅ Consistent across environments
- ✅ Easy rollback

### Cons
- ❌ Requires elevated capabilities
- ❌ Host network required
- ❌ tracefs access complexity
- ❌ Security concerns (CAP_SYS_ADMIN)

### Requirements
```bash
# Docker flags
--network host       # See host traffic
--cap-add CAP_SYS_ADMIN
--cap-add CAP_NET_RAW
-v /sys/kernel/tracing:/sys/kernel/tracing:ro
```

### Installation
```bash
# Build image
docker build -t netmon:latest .

# Run container
docker run -d \
  --name netmon \
  --network host \
  --cap-add CAP_SYS_ADMIN \
  --cap-add CAP_NET_RAW \
  -v /sys/kernel/tracing:/sys/kernel/tracing:ro \
  netmon:latest

# Or use compose
docker-compose up -d
```

### Management
```bash
docker-compose ps
docker-compose logs -f netmon
docker-compose restart netmon
```

### Best For
- Development/testing
- Docker Swarm clusters
- Environments with container standards
- Quick proof-of-concept

---

## Method 3: Kubernetes DaemonSet (Recommended for K8s)

### Pros
- ✅ Automatic scaling (one per node)
- ✅ Self-healing
- ✅ Rolling updates
- ✅ Centralized management
- ✅ Integration with K8s ecosystem
- ✅ Resource quotas

### Cons
- ❌ Complex setup
- ❌ Requires K8s cluster
- ❌ Overhead for small deployments
- ❌ Security context configuration

### Requirements
```yaml
hostNetwork: true
securityContext:
  capabilities:
    add:
    - CAP_SYS_ADMIN
    - CAP_NET_RAW
```

### Installation
```bash
# Deploy
kubectl apply -f k8s/daemonset.yaml
kubectl apply -f k8s/configmap.yaml

# Verify
kubectl get daemonset netmon
kubectl get pods -l app=netmon
```

### Management
```bash
# View logs
kubectl logs -l app=netmon -f

# Rolling restart
kubectl rollout restart daemonset/netmon

# Update config
kubectl apply -f k8s/configmap.yaml
kubectl rollout restart daemonset/netmon
```

### Best For
- Kubernetes clusters
- Multi-node deployments
- Cloud-native environments
- Auto-scaling requirements

---

## Method 4: Standalone Binary

### Pros
- ✅ Simplest deployment
- ✅ No dependencies
- ✅ Maximum performance
- ✅ Full control

### Cons
- ❌ Manual management
- ❌ No auto-restart
- ❌ No service integration
- ❌ Manual log handling

### Requirements
```bash
# Just the binary
chmod +x netmon
```

### Installation
```bash
# Build
make build

# Run
sudo ./bin/netmon --config config.yaml

# Or background
nohup sudo ./bin/netmon --config config.yaml &
```

### Management
```bash
# Manual
kill -HUP $(pgrep netmon)  # Reload
kill $(pgrep netmon)       # Stop
```

### Best For
- Testing/development
- Custom deployments
- Embedded scenarios
- Minimal environments

---

## Security Comparison

| Method | Isolation | Capabilities | Root Required | Notes |
|--------|-----------|--------------|---------------|-------|
| **Systemd** | Process | Specific | Yes | Hardened with security options |
| **Docker** | Container | Specific | No* | *Container root, not host |
| **Kubernetes** | Pod | Specific | No* | *Pod security context |
| **Binary** | None | All | Yes | Full host access |

---

## Performance Comparison

| Method | CPU Overhead | Memory Overhead | Network | Notes |
|--------|--------------|-----------------|---------|-------|
| **Systemd** | ~0% | ~0% | Native | Baseline |
| **Docker** | ~1-2% | ~10-20MB | Native | Host network |
| **Kubernetes** | ~2-5% | ~20-50MB | Native | Pod overhead |
| **Binary** | ~0% | ~0% | Native | Same as systemd |

---

## Resource Requirements

### Minimum

| Resource | Requirement |
|----------|-------------|
| CPU | 1 core |
| Memory | 128 MB |
| Disk | 50 MB |
| Network | Host access |

### Recommended

| Resource | Requirement |
|----------|-------------|
| CPU | 2 cores |
| Memory | 256-512 MB |
| Disk | 100 MB |
| Network | 1 Gbps |

### High Traffic

| Resource | Requirement |
|----------|-------------|
| CPU | 4+ cores |
| Memory | 1-2 GB |
| Disk | 500 MB+ |
| Network | 10 Gbps |

---

## Configuration Management

| Method | Config Updates | Reload | Version Control |
|--------|---------------|--------|-----------------|
| **Systemd** | Edit file | SIGHUP | Manual/Ansible |
| **Docker** | Volume mount | Restart | Image tags |
| **Kubernetes** | ConfigMap | Rolling | GitOps |
| **Binary** | Edit file | SIGHUP | Manual |

---

## Monitoring Integration

### Prometheus

All methods expose metrics on port 9876:

```yaml
# prometheus.yml
scrape_configs:
  - job_name: 'netmon'
    static_configs:
      - targets: ['localhost:9876']
```

### Grafana

Import dashboard from `grafana/dashboard.json`

### Alerts

```yaml
# alertmanager.yml
groups:
  - name: netmon
    rules:
      - alert: HighTCPLoss
        expr: rate(netmon_tcp_loss_total[5m]) > 100
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "High TCP loss detected"
```

---

## Decision Matrix

### Choose Systemd if:
- ✅ Single Linux server
- ✅ Production environment
- ✅ Need reliability (auto-restart)
- ✅ No container orchestration

### Choose Docker if:
- ✅ Development/testing
- ✅ Docker Swarm cluster
- ✅ Need quick deployment
- ✅ Container standards required

### Choose Kubernetes if:
- ✅ K8s cluster available
- ✅ Multi-node deployment
- ✅ Need auto-scaling
- ✅ GitOps workflow

### Choose Binary if:
- ✅ Testing/development
- ✅ Custom integration
- ✅ Minimal dependencies
- ✅ Embedded scenario

---

## Migration Paths

### Binary → Systemd
```bash
# Stop binary
pkill netmon

# Install service
sudo make install

# Start service
sudo systemctl start netmon
```

### Docker → Systemd
```bash
# Stop container
docker-compose down

# Install service
sudo make install

# Start service
sudo systemctl start netmon
```

### Systemd → Kubernetes
```bash
# Export config
kubectl create configmap netmon-config --from-file=config.yaml

# Deploy
kubectl apply -f k8s/daemonset.yaml

# Remove systemd
sudo make uninstall
```

---

## Troubleshooting

### Systemd
```bash
# Status
systemctl status netmon

# Logs
journalctl -u netmon -f

# Test config
netmon --config /etc/netmon/config.yaml --test
```

### Docker
```bash
# Status
docker-compose ps

# Logs
docker-compose logs -f netmon

# Inspect
docker inspect netmon
```

### Kubernetes
```bash
# Status
kubectl get daemonset netmon

# Logs
kubectl logs -l app=netmon -f

# Describe
kubectl describe daemonset netmon
```

---

## Summary

**Production Recommendations:**

1. **Single Server:** Systemd service
2. **Multiple Servers:** Kubernetes DaemonSet
3. **Development:** Docker Compose
4. **Testing:** Standalone binary

**Security Best Practices:**

1. Use specific capabilities (not --privileged)
2. Run as non-root where possible
3. Enable security options (read-only fs, no-new-privileges)
4. Regular updates
5. Monitor the monitor!

---

*Choose the right deployment method for your environment!*
