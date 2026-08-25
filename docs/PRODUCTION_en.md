# Production Deployment Guide — netmon

> **Purpose:** Production-ready deployment of **netmon** for TCP packet loss monitoring (retransmits) with IP role/location labeling.
>
> **Current version:** Uses eBPF `tcp_retransmit_skb` tracepoint as the default data source (production-ready since v1.0).

---

## Table of Contents

1. [System Requirements](#1-system-requirements)
2. [Configuration](#2-configuration)
3. [systemd Service](#3-systemd-service)
4. [Health & Readiness](#4-health--readiness)
5. [Metrics & Alerts](#5-metrics--alerts)
6. [Limitations](#6-limitations)

---

## 1. System Requirements

### Kernel Requirements

| Component | Requirement | Notes |
|-----------|-------------|-------|
| **OS / architecture** | Linux x86_64 (`amd64`) | ARM builds are not published or supported |
| **Kernel** | **5.8+ minimum**, tested 5.15 / 6.1 / 6.8 / 6.12 | Ring buffer (`BPF_MAP_TYPE_RINGBUF`) requires 5.8+. On `<5.8` the eBPF path is unavailable — use `loss_source: tracepipe` |
| **BTF** | `/sys/kernel/btf/vmlinux` must exist | CO-RE requires kernel BTF information |

**Check BTF:**
```bash
ls -la /sys/kernel/btf/vmlinux
# Should output file info, not "No such file"
```

### Capabilities Required

netmon uses eBPF for TCP loss collection. The following capabilities are required:

| Capability | Purpose | Notes |
|------------|---------|-------|
| `CAP_SYS_ADMIN` | Load eBPF program **and** attach the `tcp_retransmit_skb` tracepoint | **Recommended.** Covers the whole eBPF + perf path on all tested kernels |
| `CAP_NET_RAW` | Traceroute (ICMP/UDP/TCP), only if discovery enabled | — |
| `CAP_BPF` + `CAP_PERFMON` | Least-privilege alternative to CAP_SYS_ADMIN | Kernel 5.8+. **May fail** — see the warning below |

**Recommended systemd configuration (verified on 5.15 / 6.1 / 6.8 / 6.12):**
```ini
AmbientCapabilities=CAP_SYS_ADMIN CAP_NET_RAW
CapabilityBoundingSet=CAP_SYS_ADMIN CAP_NET_RAW
```

**Least-privilege alternative (verify on your kernel first):**
```ini
AmbientCapabilities=CAP_BPF CAP_PERFMON CAP_NET_RAW
CapabilityBoundingSet=CAP_BPF CAP_PERFMON CAP_NET_RAW
```

> ⚠️ **Why not CAP_BPF + CAP_PERFMON by default?** The collector attaches the
> `tcp_retransmit_skb` tracepoint via `perf_event_open`. On kernels with
> `kernel.perf_event_paranoid > 1` (Debian/Ubuntu default is 3/4), that attach is
> **not** granted by CAP_BPF + CAP_PERFMON — it fails with
> `opening tracepoint perf event: permission denied`. The program *loads* but never
> attaches. `CAP_SYS_ADMIN` covers this path and is what we recommend. If you must
> use the split caps, either verify the attach succeeds on your kernel or lower the
> gate with `sysctl kernel.perf_event_paranoid=1`.
>
> **Note:** the systemd unit runs as `User=root`; the capability bounding set still
> restricts what that root process may do, so CAP_SYS_ADMIN here is the eBPF/perf
> capability, not unrestricted root.

### File System Requirements

| Path | Purpose | Permissions |
|------|---------|-------------|
| `/sys/kernel/btf/vmlinux` | Kernel BTF | Read |
| `/sys/kernel/tracing/trace_pipe` | Legacy fallback (optional) | Read |
| `/etc/netmon/` | Configuration directory | Read |
| `/var/lib/netmon/` | Working directory | Read/Write |
| `/var/log/netmon/` | Log files (if file logging enabled) | Write |

---

## 2. Configuration

### Main Configuration (config.yaml)

```yaml
global:
  # TCP loss data source: "ebpf" (default, production) or "tracepipe" (legacy/fallback)
  loss_source: ebpf
  
  # Metrics HTTP server bind address (validate as valid IP)
  metrics_addr: "0.0.0.0"
  
  # Metrics HTTP server port
  metrics_port: 9876
  
  # Optional authentication token for /metrics and /api/* endpoints
  # Can also be set via NETMON_AUTH_TOKEN environment variable
  auth_token: ""  # Recommended: set via env NETMON_AUTH_TOKEN
  
  # TTL for in-memory metrics (hours)
  ttl_hours: 3
  
  # Legacy trace_pipe path (only used if loss_source: tracepipe)
  trace_pipe_path: /sys/kernel/tracing/trace_pipe

metadata:
  locations:
    path: /etc/netmon/locations.yaml
    # Optional HTTP auto-update:
    # update_source:
    #   url: https://config.example.com/locations.yaml
    #   poll_interval: 20m
    #   timeout: 10s
  roles:
    path: /etc/netmon/roles.yaml
  topology:
    path: /etc/netmon/topology.yaml
  unknown:
    enabled: true
    ttl: 3h
    max_ips: 10000

metrics:
  # Recommended production labels: VRF remains available without per-IP labels.
  default_labels: [src_location, dst_location, src_role, dst_role, src_vrf, dst_vrf]
  optional_labels: []
  # Cardinality control for loss metrics
  cardinality:
    # Level: "ip" | "role" | "network"
    #   - ip: label every series with src_ip/dst_ip (unbounded, NOT recommended for large networks)
    #   - network: aggregate to /24 networks (no per-IP labels)
    #   - role: aggregate to location/role/vrf (no IP, no network) [DEFAULT]
    level: role
    
    # Hard cap on active series (0 = unlimited)
    # Prevents OOM on Prometheus and netmon itself
    max_series: 10000

logging:
  level: info
  format: json
  # output_path: /var/log/netmon/netmon.log  # Empty = stdout/stderr

discovery:
  traceroute:
    enabled: true
    mode: both  # both | top_loss | on_demand | periodic
    protocol: icmp  # only production-supported protocol
    interval: 5m
    top_n: 10
    max_hops: 30
    timeout: 3s
    probes_per_hop: 3
```

### ⚠️ Critical: Cardinality Level

**Default `level: role` does NOT include `src_ip`/`dst_ip` in metric labels.**

This is intentional to prevent cardinality explosion in large networks. The effective label set comes from `default_labels` plus `optional_labels`, while `cardinality.level` limits the maximum permitted detail:

| Level | Permitted labels on `netmon_tcp_loss_total` |
|-------|-----------------------------------|
| `role` (default) | `src_location, dst_location, src_role, dst_role, src_vrf, dst_vrf` (no IP, no network) |
| `network` | `src_network, dst_network, src_location, dst_location, src_role, dst_role, src_vrf, dst_vrf` (/24, no IP) |
| `ip` | `src_ip, dst_ip, src_location, dst_location, src_role, dst_role, src_network, dst_network, src_vrf, dst_vrf` (unbounded) |

Only labels explicitly listed in `default_labels` or `optional_labels` and
permitted by the selected level are exported. For example, with `level: ip`,
removing `src_network` and `dst_network` from `optional_labels` removes them
from Prometheus output without affecting `src_ip` and `dst_ip`.

`src_vrf` and `dst_vrf` are populated from the `vrf` attribute of the matching
entry in `locations.yaml`; at `level: role` they split loss series by VRF
without adding IP cardinality. Changes to the label list require restart.

Unknown role/location/VRF addresses remain discoverable even without IP labels:

```bash
curl http://localhost:9876/api/v1/metadata/unknown
curl http://localhost:9876/metrics/metadata/unknown
```

The first endpoint is a bounded JSON inventory. The second is an opt-in
per-IP Prometheus endpoint; the main `/metrics` endpoint contains only bounded
`netmon_metadata_unknown_ips{attribute}` aggregates.

**To get per-IP metrics (use with caution):**
```yaml
metrics:
  cardinality:
    level: ip  # ⚠️ Creates one series per unique IP pair
    max_series: 100000  # Increase cap if needed
```

> ⚠️ **Warning:** Setting `level: ip` in a large network can create thousands of series and cause OOM on both netmon and Prometheus. Use `role` or `network` level for production.

### IP Role/Location Mapping (Longest Prefix Match)

netmon uses **longest prefix match** for IP → role/location lookup. More specific routes win:

**Example:**
```yaml
# roles.yaml
roles:
  - role: datacenter
    networks: [10.179.64.0/22, 10.180.0.0/20]
  - network: 10.179.64.32/32   # Legacy single entry is also supported
    role: s3-dwh05
```

Both `roles.yaml` and `locations.yaml` accept either `network` or `networks`.
The fields are mutually exclusive, and every value must use CIDR notation.
Grouped entries use the same longest-prefix matching as single entries.
For IP `10.179.64.32`, the more-specific `s3-dwh05` role wins.

**See example configs:**
- [`configs/roles.example.yaml`](../configs/roles.example.yaml)
- [`configs/locations.example.yaml`](../configs/locations.example.yaml)

### Authentication

**Set auth token via environment variable (recommended):**
```bash
export NETMON_AUTH_TOKEN="your-secret-token-here"
sudo systemctl restart netmon
```

**Or in config file:**
```yaml
global:
  auth_token: "your-secret-token-here"
```

**Protected endpoints:** `/metrics`, `/api/*`
**Public endpoints:** `/health`, `/ready`

Telemetry integrity: monitor
`netmon_loss_events_dropped_total{reason="ringbuf_full"}`. Any increase means the
kernel ring buffer was full and the primary loss metric is under-reporting;
treat this as monitoring degradation.

**Example curl with auth:**
```bash
curl -H "Authorization: Bearer your-secret-token-here" http://localhost:9876/metrics
```

---

## 3. systemd Service

### Example Unit File (`/etc/systemd/system/netmon.service`)

```ini
[Unit]
Description=Network Monitor - TCP Packet Loss Tracking (eBPF)
Documentation=https://github.com/vponomarev/network-monitor/blob/main/docs/PRODUCTION_en.md
After=network.target network-online.target
Wants=network-online.target

[Service]
Type=simple
User=root
Group=root

# Binary and config
ExecStart=/usr/local/bin/netmon --config /etc/netmon/config.yaml
ExecReload=/bin/kill -HUP $MAINPID

# Restart policy (important with TASK-09 fatal error handling)
# On fatal collector error, netmon exits with code 1 → systemd restarts it
Restart=on-failure
RestartSec=5
StartLimitIntervalSec=60
StartLimitBurst=5

# Environment
Environment=PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
Environment=GOMAXPROCS=2
# Environment=NETMON_AUTH_TOKEN=your-secret-token-here

# Working directory
WorkingDirectory=/var/lib/netmon

# Security hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=read-only
PrivateTmp=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true

# Capabilities. CAP_SYS_ADMIN covers the eBPF load + tracepoint (perf) attach on
# all tested kernels; CAP_NET_RAW is only for traceroute (discovery).
AmbientCapabilities=CAP_SYS_ADMIN CAP_NET_RAW
CapabilityBoundingSet=CAP_SYS_ADMIN CAP_NET_RAW

# Least-privilege alternative (may fail to attach the tracepoint on kernels with
# perf_event_paranoid > 1 — see "Capabilities Required" above):
# AmbientCapabilities=CAP_BPF CAP_PERFMON CAP_NET_RAW
# CapabilityBoundingSet=CAP_BPF CAP_PERFMON CAP_NET_RAW

# File descriptors
LimitNOFILE=65536
LimitNPROC=4096

# Memory limit (optional, adjust based on cardinality)
MemoryMax=512M

# Logging
StandardOutput=journal
StandardError=journal
SyslogIdentifier=netmon

[Install]
WantedBy=multi-user.target
```

### Installation

```bash
# Copy service file
sudo cp packaging/netmon.service /etc/systemd/system/netmon.service

# Reload systemd
sudo systemctl daemon-reload

# Enable and start
sudo systemctl enable netmon
sudo systemctl start netmon

# Verify
sudo systemctl status netmon
```

### Configuration Reload

```bash
# Graceful reload (SIGHUP) - reloads config without dropping connections
sudo systemctl reload netmon

# Full restart (if binary updated)
sudo systemctl restart netmon
```

### Resource Limits

| Resource | Default | Recommended (production) |
|----------|---------|-------------------------|
| Memory | Unlimited | `MemoryMax=512M` (adjust for cardinality) |
| CPU | Unlimited | `CPUQuota=100%` (1 core) |
| File descriptors | 65536 | 65536 |
| Restart delay | 5s | 5s (with burst limit) |

---

## 4. Health & Readiness

### Endpoints

| Endpoint | Method | Response | Purpose |
|----------|--------|----------|---------|
| `/health` | GET | `200 OK` (always, if process alive) | **Liveness** probe |
| `/ready` | GET | `200 OK` (collector running) or `503` (not ready) | **Readiness** probe |

### Response Examples

**Health (always 200 when HTTP server is running):**
```json
{"status":"ok"}
```

**Ready (collector running):**
```json
{"status":"ready"}
```

**Not Ready (collector not started or stopped):**
```json
{"status":"not ready","reason":"loss collector not started"}
```

### Kubernetes Probes

```yaml
livenessProbe:
  httpGet:
    path: /health
    port: 9876
  initialDelaySeconds: 5
  periodSeconds: 10
  timeoutSeconds: 3
  failureThreshold: 3

readinessProbe:
  httpGet:
    path: /ready
    port: 9876
  initialDelaySeconds: 5
  periodSeconds: 5
  timeoutSeconds: 3
  failureThreshold: 3
```

### systemd Integration

```ini
# In [Service] section
# netmon signals readiness via /ready endpoint
# Use ExecStartPost to wait for readiness before considering service started

ExecStartPost=/bin/sh -c 'for i in $(seq 1 30); do curl -sf http://localhost:9876/ready && exit 0; sleep 1; done; exit 1'
```

---

## 5. Metrics & Alerts

### Core Metric: TCP Loss

```promql
# TCP retransmit count (one event = one retransmitted segment)
# Label set below is for the DEFAULT cardinality level "role":
netmon_tcp_loss_total{
    src_location="datacenter-a",
    dst_location="datacenter-b",
    src_role="app-server",
    dst_role="database",
    src_vrf="unknown",
    dst_vrf="unknown"
}
```

> ⚠️ **Note:** After TASK-01, this metric correctly increments by +1 per retransmit (not by packet count).

### Collector Self-Metrics (TASK-08)

```promql
# Collector status: 1 = running, 0 = stopped/failed
netmon_loss_collector_up

# Events read from kernel (ring buffer or trace_pipe)
netmon_loss_events_read_total

# Events successfully parsed
netmon_loss_events_parsed_total

# Events failed to parse (indicates format mismatch or corruption)
netmon_loss_parse_errors_total

# Loss source info (ebpf or tracepipe)
netmon_loss_source_info{source="ebpf"}  # 1 if active
```

### Cardinality Metrics (TASK-10)

```promql
# Current number of active series (before export to Prometheus)
netmon_loss_active_series

# Cumulative count of series dropped due to max_series limit
netmon_loss_series_dropped_total
```

### PromQL Alert Rules

```yaml
groups:
  - name: netmon
    rules:
      # Alert 1: Collector is down
      - alert: NetmonCollectorDown
        expr: netmon_loss_collector_up == 0
        for: 2m
        labels:
          severity: critical
        annotations:
          summary: "netmon collector is down"
          description: "netmon on {{ $labels.instance }} has stopped collecting data"

      # Alert 2: Parse errors (format mismatch)
      - alert: NetmonParseErrors
        expr: rate(netmon_loss_parse_errors_total[5m]) > 0
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "netmon parse errors detected"
          description: "netmon is failing to parse loss events on {{ $labels.instance }}"

      # Alert 2b: Reading events but none parse (event format drift)
      - alert: NetmonReadNotParsed
        expr: rate(netmon_loss_events_read_total[5m]) > 0 and rate(netmon_loss_events_parsed_total[5m]) == 0
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "netmon reads loss events but parses none"
          description: "Likely eBPF/Go struct drift on {{ $labels.instance }} — check kernel/version"

      # Alert 3: Cardinality limit reached
      - alert: NetmonSeriesDropped
        expr: increase(netmon_loss_series_dropped_total[1h]) > 0
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "netmon dropping series due to cardinality limit"
          description: "Increase metrics.cardinality.max_series on {{ $labels.instance }}"

      # Alert 4: High loss rate (example, adjust thresholds)
      - alert: NetmonHighLossRate
        expr: rate(netmon_tcp_loss_total[5m]) > 100  # Adjust threshold
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "High TCP loss rate detected"
          description: "Path {{ $labels.src_role }} → {{ $labels.dst_role }} has {{ $value }} retransmits/sec"
```

### Grafana Dashboard

See [`dashboards/`](../dashboards/) for pre-built Grafana dashboard JSON.

---

## 6. Limitations

### IPv4 Only

**Current version supports IPv4 only.** IPv6 is not implemented in this release.

- eBPF program: `bpf/tcploss.bpf.c` handles IPv4 only
- Metadata matching: IPv4 CIDR only

### Retransmit as Loss Proxy

**This tool measures TCP retransmits, not absolute packet loss.**

Retransmitted SYN and SYN-ACK handshake packets are excluded. A failed outbound
connection attempt therefore does not create `netmon_tcp_loss_total`; the
metric represents retransmits after TCP connection establishment.

A retransmit indicates a packet was lost **somewhere on the path**, but:
- One retransmit ≠ one lost packet (could be spurious retransmit)
- Multiple retransmits per original packet are counted separately
- Retransmits can occur due to congestion, not just loss

**Use this metric as a relative indicator, not absolute loss percentage.**

### Cardinality Limits

**Default `max_series: 10000` prevents unbounded growth.**

If you see `netmon_loss_series_dropped_total > 0`:
1. Check `netmon_loss_active_series` — how many series are active?
2. If legitimate traffic: increase `max_series` in config
3. If unexpected: investigate traffic patterns, consider `level: role` instead of `ip`

### Kernel Compatibility

| Kernel Version | Status | Notes |
|----------------|--------|-------|
| **6.12** | ✅ Tested | Debian 13 |
| **6.8** | ✅ Tested | Proxmox 8.4 (Debian 12 base) |
| **6.1** | ✅ Tested | Debian 12 |
| **5.15** | ✅ Tested | Ubuntu 22.04 |
| **5.8–5.14** | ⚙️ Supported (untested here) | Ring buffer available; CO-RE requires BTF present |
| **<5.8** | ❌ eBPF path unavailable | No `BPF_MAP_TYPE_RINGBUF` — use `loss_source: tracepipe` |
| **<4.9** | ❌ Unsupported | No eBPF |

---

## Quick Start Checklist

```bash
# 1. Check kernel BTF
ls /sys/kernel/btf/vmlinux

# 2. Install binary
wget https://github.com/vponomarev/network-monitor/releases/latest/download/netmon-linux-amd64
sudo cp netmon-linux-amd64 /usr/local/bin/netmon
sudo chmod +x /usr/local/bin/netmon

# 3. Create directories
sudo mkdir -p /etc/netmon /var/lib/netmon /var/log/netmon

# 4. Copy config
sudo cp configs/config.example.yaml /etc/netmon/config.yaml
# Edit /etc/netmon/config.yaml as needed

# 5. Copy roles/locations
sudo cp configs/roles.example.yaml /etc/netmon/roles.yaml
sudo cp configs/locations.example.yaml /etc/netmon/locations.yaml

# 6. Install systemd service
sudo cp packaging/netmon.service /etc/systemd/system/netmon.service
sudo systemctl daemon-reload
sudo systemctl enable netmon
sudo systemctl start netmon

# 7. Verify
sudo systemctl status netmon
curl http://localhost:9876/health
curl http://localhost:9876/ready
curl http://localhost:9876/metrics | grep netmon_tcp_loss
```

---

## Troubleshooting

### Collector Not Starting

```bash
# Check logs
sudo journalctl -u netmon -n 50

# Common errors:
# - "BTF not found" → Install kernel headers or enable BTF
# - "attaching tracepoint ... opening tracepoint perf event: permission denied"
#     → CAP_BPF+CAP_PERFMON is insufficient on this kernel (perf_event_paranoid>1).
#       Use CAP_SYS_ADMIN CAP_NET_RAW (recommended), or lower perf_event_paranoid.
# - "permission denied" (other) → Check capabilities in systemd unit
# - "config invalid" → Validate YAML syntax
```

```bash
# 1. Confirm the collector attached (eBPF path enables the tracepoint itself —
#    you do NOT need to echo 1 to the enable file; that is only for tracepipe).
curl -s http://localhost:9876/metrics | grep -E 'netmon_loss_collector_up|netmon_loss_source_info'
#   netmon_loss_collector_up 1
#   netmon_loss_source_info{source="ebpf"} 1

# 2. netmon_tcp_loss_total only appears once at least one retransmit happens.
#    Generate loss to test (lab only), e.g. on a spare interface:
#    sudo tc qdisc add dev <iface> root netem loss 20%   (remove with: tc qdisc del dev <iface> root)

# 3. read grows but parsed does not -> event format drift (see NetmonParseErrors):
curl -s http://localhost:9876/metrics | grep -E 'netmon_loss_events_read_total|netmon_loss_events_parsed_total|netmon_loss_parse_errors_total'
```

> Legacy `loss_source: tracepipe` only: the text tracepoint must be enabled
> (`echo 1 | sudo tee /sys/kernel/tracing/events/tcp/tcp_retransmit_skb/enable`,
> or run netmon with `--enable-tracing`). The eBPF path does not need this.

### High Memory Usage

```bash
# Check active series
curl http://localhost:9876/metrics | grep netmon_loss_active_series

# Reduce cardinality
# Edit /etc/netmon/config.yaml:
# metrics:
#   cardinality:
#     level: role  # or "network"
#     max_series: 5000

sudo systemctl reload netmon
```

---

## See Also

- [Configuration Guide](configuration.md) — Full config reference
- [eBPF Development Guide](ebpf-guide.md) — eBPF program details
- [systemd Deployment](SYSTEMD_DEPLOYMENT.md) — Legacy systemd guide (trace_pipe)
- [Docker Deployment](DOCKER_DEPLOYMENT.md) — Container deployment

---

*Last updated: July 2026 | netmon v1.0+ (eBPF)*
