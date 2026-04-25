# Docker Deployment Guide

## Overview

Network Monitor **can** run in Docker, but requires elevated privileges for kernel access.

---

## Requirements

### Kernel Requirements

| Requirement | Description | Check Command |
|-------------|-------------|---------------|
| **tracefs** | Kernel tracing filesystem | `mount -t tracefs none /sys/kernel/tracing` |
| **Host network** | See host network traffic | `--network host` |
| **Raw sockets** | For traceroute | `CAP_NET_RAW` |

### Docker Requirements

| Component | Requirement | Why |
|-----------|-------------|-----|
| **Network** | `--network host` | See all host TCP connections |
| **Capabilities** | `CAP_SYS_ADMIN` | Access trace_pipe |
| **Capabilities** | `CAP_NET_RAW` | Raw sockets for traceroute |
| **Volume** | `/sys/kernel/tracing` | Kernel trace events |

---

## Quick Start

### Option 1: Docker Compose (Recommended)

```bash
# Build and run
docker-compose up -d

# Check status
docker-compose ps

# View logs
docker-compose logs -f netmon

# Stop
docker-compose down
```

### Option 2: Docker Run

```bash
docker run -d \
  --name netmon \
  --network host \
  --cap-add CAP_SYS_ADMIN \
  --cap-add CAP_NET_RAW \
  -v /sys/kernel/tracing:/sys/kernel/tracing:ro \
  -v $(pwd)/config.yaml:/etc/netmon/config.yaml:ro \
  --restart unless-stopped \
  netmon:latest
```

### Option 3: Privileged Mode (Not Recommended for Production)

```bash
docker run -d \
  --name netmon \
  --privileged \
  --network host \
  netmon:latest
```

⚠️ **Warning:** `--privileged` grants ALL capabilities - security risk!

---

## Security Considerations

### Capability Breakdown

| Capability | Purpose | Risk Level |
|------------|---------|------------|
| `CAP_SYS_ADMIN` | Access trace_pipe, mount operations | 🔴 High |
| `CAP_NET_RAW` | Raw sockets for traceroute | 🟡 Medium |

### Risk Mitigation

1. **Use specific capabilities** instead of `--privileged`
2. **Read-only root filesystem** where possible
3. **Non-root user** inside container
4. **Resource limits** (CPU, memory)
5. **Network policies** (restrict outbound)

### Secure Configuration

```yaml
# docker-compose.secure.yml
services:
  netmon:
    # Use specific capabilities
    cap_add:
      - CAP_SYS_ADMIN
      - CAP_NET_RAW

    # Drop all other capabilities
    cap_drop:
      - ALL

    # Read-only root filesystem
    read_only: true

    # Temporary filesystems for writable paths
    tmpfs:
      - /tmp:mode=1777,size=64M

    # No new privileges
    security_opt:
      - no-new-privileges:true

    # Resource limits
    deploy:
      resources:
        limits:
          cpus: '1.0'
          memory: 512M
```

---

## Kubernetes Deployment

### DaemonSet (Recommended)

```yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: netmon
  namespace: monitoring
  labels:
    app: netmon
spec:
  selector:
    matchLabels:
      app: netmon
  template:
    metadata:
      labels:
        app: netmon
    spec:
      # Run on host network
      hostNetwork: true

      # Run on host PID namespace (optional, for process info)
      # hostPID: true

      containers:
      - name: netmon
        image: netmon:latest
        imagePullPolicy: Always

        args:
        - --config
        - /etc/netmon/config.yaml

        ports:
        - name: metrics
          containerPort: 9876
          hostPort: 9876
          protocol: TCP

        securityContext:
          capabilities:
            add:
            - CAP_SYS_ADMIN
            - CAP_NET_RAW
          readOnlyRootFilesystem: true
          runAsNonRoot: false  # Required for CAP_SYS_ADMIN
          runAsUser: 0

        volumeMounts:
        - name: tracefs
          mountPath: /sys/kernel/tracing
          readOnly: true
        - name: config
          mountPath: /etc/netmon
          readOnly: true
        - name: data
          mountPath: /var/lib/netmon

        resources:
          limits:
            cpu: 1000m
            memory: 512Mi
          requests:
            cpu: 250m
            memory: 128Mi

        livenessProbe:
          httpGet:
            path: /health
            port: 9876
          initialDelaySeconds: 10
          periodSeconds: 30
          timeoutSeconds: 10
          failureThreshold: 3

        readinessProbe:
          httpGet:
            path: /ready
            port: 9876
          initialDelaySeconds: 5
          periodSeconds: 10
          timeoutSeconds: 5
          failureThreshold: 3

      volumes:
      - name: tracefs
        hostPath:
          path: /sys/kernel/tracing
          type: Directory
      - name: config
        configMap:
          name: netmon-config
      - name: data
        emptyDir: {}

      # Tolerations for running on master nodes (optional)
      # tolerations:
      # - key: node-role.kubernetes.io/master
      #   effect: NoSchedule
```

### ConfigMap

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: netmon-config
  namespace: monitoring
data:
  config.yaml: |
    global:
      ttl_hours: 3
      metrics_port: 9876
      trace_pipe_path: /sys/kernel/tracing/trace_pipe

    discovery:
      traceroute:
        enabled: true
        protocol: tcp
        dst_port: 443

    topology:
      enabled: false
      path: /etc/netmon/topology.yaml

    logging:
      level: info
      format: json
```

### ServiceMonitor (Prometheus Operator)

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: netmon
  namespace: monitoring
  labels:
    app: netmon
spec:
  selector:
    matchLabels:
      app: netmon
  namespaceSelector:
    matchNames:
    - monitoring
  endpoints:
  - port: metrics
    interval: 30s
    path: /metrics
```

---

## Troubleshooting

### trace_pipe Not Accessible

**Error:** `trace_pipe not found`

**Solutions:**
```bash
# On host, mount tracefs
sudo mount -t tracefs none /sys/kernel/tracing

# Verify
ls -la /sys/kernel/tracing/trace_pipe

# In Docker, ensure volume mount
docker run -v /sys/kernel/tracing:/sys/kernel/tracing:ro ...
```

### Permission Denied

**Error:** `permission denied`

**Solutions:**
```bash
# Add capabilities
docker run --cap-add CAP_SYS_ADMIN ...

# Or use privileged (not recommended)
docker run --privileged ...
```

### No Network Traffic Visible

**Problem:** Container sees no TCP retransmits

**Solutions:**
```bash
# Must use host network
docker run --network host ...

# NOT bridge network (default)
docker run --network bridge ...  # ❌ Won't work!
```

### Traceroute Fails

**Error:** `creating raw socket: operation not permitted`

**Solutions:**
```bash
# Add CAP_NET_RAW
docker run --cap-add CAP_NET_RAW ...

# Or use privileged
docker run --privileged ...
```

---

## Performance

### Resource Usage

| Metric | Typical | Maximum |
|--------|---------|---------|
| CPU | 50-200m | 1000m |
| Memory | 64-128MB | 512MB |
| Network | <1Mbps | 10Mbps |
| Disk | <10MB | 100MB |

### Optimization Tips

1. **Limit trace_pipe events:**
   ```bash
   # Only enable tcp_retransmit_skb
   echo 0 > /sys/kernel/tracing/events/tcp/enable
   echo 1 > /sys/kernel/tracing/events/tcp/tcp_retransmit_skb/enable
   ```

2. **Adjust metrics TTL:**
   ```yaml
   global:
     ttl_hours: 1  # Reduce from default 3
   ```

3. **Limit discovery:**
   ```yaml
   discovery:
     traceroute:
       enabled: false  # Disable if not needed
   ```

---

## Alternative: Host Installation

If Docker is too complex, install directly on host:

```bash
# Download binary
wget https://github.com/vponomarev/network-monitor/releases/latest/download/netmon-linux-amd64

# Make executable
chmod +x netmon-linux-amd64
sudo mv netmon-linux-amd64 /usr/local/bin/netmon

# Create systemd service
sudo tee /etc/systemd/system/netmon.service > /dev/null <<EOF
[Unit]
Description=Network Monitor
After=network.target

[Service]
Type=simple
User=root
ExecStart=/usr/local/bin/netmon --config /etc/netmon/config.yaml
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

# Enable and start
sudo systemctl daemon-reload
sudo systemctl enable netmon
sudo systemctl start netmon
sudo systemctl status netmon
```

---

## Comparison: Docker vs Host

| Aspect | Docker | Host |
|--------|--------|------|
| **Isolation** | ✅ Container | ❌ None |
| **Deployment** | ✅ Easy | ⚠️ Manual |
| **Security** | ⚠️ Capabilities | 🔴 Root |
| **Performance** | ✅ Same | ✅ Same |
| **Updates** | ✅ Image pull | ⚠️ Manual |
| **Orchestration** | ✅ K8s/Swarm | ⚠️ Custom |
| **Debugging** | ⚠️ Container exec | ✅ Direct |

**Recommendation:** Use Docker for orchestration (K8s), host for simple deployments.

---

## Files

| File | Purpose |
|------|---------|
| `Dockerfile` | Multi-stage build |
| `docker-compose.yml` | Local development |
| `k8s/daemonset.yaml` | Kubernetes deployment |
| `k8s/configmap.yaml` | Kubernetes config |
| `k8s/servicemonitor.yaml` | Prometheus integration |

---

## Summary

**Can it run in Docker?** ✅ Yes, with caveats

**Requirements:**
- `--network host` (mandatory)
- `CAP_SYS_ADMIN` (for trace_pipe)
- `CAP_NET_RAW` (for traceroute)
- `/sys/kernel/tracing` volume mount

**Best for:**
- ✅ Kubernetes clusters
- ✅ Docker Swarm
- ✅ Development/testing

**Consider host install for:**
- Single server deployments
- Maximum performance
- Simpler security model

---

*Ready for production deployment!*
