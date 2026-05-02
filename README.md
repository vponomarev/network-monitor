# Network Monitor

[![CI](https://github.com/vponomarev/network-monitor/actions/workflows/ci.yml/badge.svg)](https://github.com/vponomarev/network-monitor/actions/workflows/ci.yml)
[![Release](https://github.com/vponomarev/network-monitor/actions/workflows/release.yml/badge.svg)](https://github.com/vponomarev/network-monitor/actions/workflows/release.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/vponomarev/network-monitor)](https://goreportcard.com/report/github.com/vponomarev/network-monitor)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

**Linux network monitoring suite** consisting of two applications:

| Application | Description | Technology |
|-------------|-------------|------------|
| **netmon** | TCP packet loss monitoring with path discovery | trace_pipe + traceroute |
| **conntrack** | Connection tracking with eBPF | eBPF kprobes + tracepoints |

---

## 📋 Table of Contents

- [Features](#-features)
- [Quick Start](#-quick-start)
- [Architecture](#-architecture)
- [Installation](#-installation)
- [Configuration](#-configuration)
- [API Reference](#-api-reference)
- [Metrics](#-metrics)
- [Documentation](#-documentation)
- [Development](#-development)

---

## ✨ Features

### Netmon (TCP Loss Monitoring)

- **TCP Retransmit Tracking** — Real-time monitoring via `/sys/kernel/tracing/trace_pipe`
- **Path Discovery** — Automatic network path discovery with traceroute (ICMP/UDP/TCP)
- **Location/Role Mapping** — Best-match IP to location/role lookup
- **Prometheus Metrics** — Export with rich labels (location, role, network)
- **HTTP API** — RESTful API for path discovery and loss analysis
- **Grafana Dashboard** — Ready-to-use dashboard included
- **SIGHUP Reload** — Reload configuration without restart

### Conntrack (Connection Tracking)

- **TCP Handshake Tracking** — Monitor SYN → SYN+ACK → ESTABLISHED
- **Incoming/Outgoing** — Separate tracking by direction
- **Process Identification** — Track which process owns each connection
- **Syslog Logging** — Structured messages in RFC 5424 format
- **Prometheus Metrics** — Connection states, events, bytes, duration
- **HTTP API** — View active connections and statistics

### Bandwidth (Network Interface Monitoring)

- **Interface Statistics** — RX/TX bytes, packets, errors, dropped
- **Throughput Calculation** — Bytes per second rates
- **Configurable Interval** — Customizable collection frequency
- **Multi-Interface** — Monitor multiple interfaces simultaneously

### Latency (RTT Monitoring)

- **UDP Latency Checks** — Measure RTT to targets via UDP
- **High Latency Alerts** — Detect performance degradation
- **Timeout Detection** — Track unreachable targets
- **Configurable Targets** — Monitor multiple endpoints

### DNS (DNS Resolution Monitoring)

- **DNS Query Testing** — Test resolution performance
- **Slow Query Alerts** — Detect DNS issues
- **Failure Detection** — Track resolution failures
- **System Resolver** — Uses system DNS configuration

---

## 🚀 Quick Start

### Prerequisites

- Linux kernel 4.9+ (for eBPF support)
- Go 1.24+ (for building)
- Root access (for trace_pipe and eBPF)
- Docker (optional, for containerized deployment)

### Option 1: Docker Compose (Recommended)

```bash
# Clone repository
git clone https://github.com/vponomarev/network-monitor.git
cd network-monitor

# Copy example configs
cp configs/*.yaml .

# Start services
docker-compose up -d

# View logs
docker-compose logs -f netmon
```

### Option 2: Binary Installation

```bash
# Download latest release
wget https://github.com/vponomarev/network-monitor/releases/latest/download/netmon-linux-amd64
chmod +x netmon-linux-amd64
sudo mv netmon-linux-amd64 /usr/local/bin/netmon

# Mount tracefs
sudo mount -t tracefs none /sys/kernel/tracing

# Run with config
sudo netmon --config config.yaml
```

### Option 3: Build from Source

```bash
# Clone and build
git clone https://github.com/vponomarev/network-monitor.git
cd network-monitor
make build

# Run
sudo ./bin/netmon
```

### Verify Installation

```bash
# Check health
curl http://localhost:9876/health

# Get metrics
curl http://localhost:9876/metrics

# Get top lossy pairs (netmon API)
curl http://localhost:9876/api/v1/loss/top?limit=5

# Get connections (conntrack API)
curl http://localhost:9876/api/v1/conntrack/connections?limit=10
```

---

## 🏗 Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                     Network Monitor Suite                        │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  ┌─────────────────────┐         ┌─────────────────────┐       │
│  │      NETMON         │         │    CONNTRACK        │       │
│  │                     │         │                     │       │
│  │  ┌───────────────┐  │         │  ┌───────────────┐  │       │
│  │  │ trace_pipe    │  │         │  │ eBPF kprobes  │  │       │
│  │  │ collector     │  │         │  │ tcp_connect   │  │       │
│  │  └───────────────┘  │         │  │ tcp_v4_rcv    │  │       │
│  │                     │         │  │ tcp_close     │  │       │
│  │  ┌───────────────┐  │         │  └───────────────┘  │       │
│  │  │ Discovery     │  │         │                     │       │
│  │  │ Traceroute    │  │         │  ┌───────────────┐  │       │
│  │  │ (ICMP/UDP/TCP)│  │         │  │ State Machine │  │       │
│  │  └───────────────┘  │         │  │ (TCP FSM)     │  │       │
│  │                     │         │  └───────────────┘  │       │
│  │  ┌───────────────┐  │         │                     │       │
│  │  │ Metadata      │  │         │  ┌───────────────┐  │       │
│  │  │ Location/Role │  │         │  │ Syslog Writer │  │       │
│  │  └───────────────┘  │         │  └───────────────┘  │       │
│  │                     │         │                     │       │
│  └──────────┬──────────┘         └──────────┬──────────┘       │
│             │                                │                  │
│             └────────────┬───────────────────┘                  │
│                          │                                      │
│              ┌───────────▼────────────┐                        │
│              │   Prometheus Metrics   │                        │
│              │   HTTP API (9876)      │                        │
│              └────────────────────────┘                        │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

---

## 📦 Installation

### System Requirements

| Component | Requirement |
|-----------|-------------|
| OS | Linux (kernel 4.9+) |
| Memory | 128MB minimum, 512MB recommended |
| CPU | 0.25 cores minimum, 1 core recommended |
| Disk | 50MB for binary, variable for logs |

### Capabilities Required

#### Netmon
- `CAP_SYS_ADMIN` — For trace_pipe access
- `CAP_NET_RAW` — For traceroute (ICMP/UDP/TCP)

#### Conntrack
- `CAP_BPF` — For eBPF programs
- `CAP_PERFMON` — For eBPF perf events
- `CAP_NET_RAW` — For raw socket access
- `CAP_SYS_ADMIN` — For various kernel operations

### Installation Methods

See [Installation Guide](docs/installation.md) for detailed instructions.

---

## ⚙️ Configuration

### Main Configuration (config.yaml)

```yaml
global:
  ttl_hours: 3
  metrics_port: 9876
  trace_pipe_path: /sys/kernel/tracing/trace_pipe

metadata:
  locations:
    path: locations.yaml
  roles:
    path: roles.yaml

discovery:
  traceroute:
    enabled: true
    top_n: 10
    mode: both  # both | top_loss | on_demand | periodic
    interval: 5m
    protocol: icmp  # icmp | udp | tcp
    max_hops: 30
    timeout: 3s

connections:
  enabled: true
  track_incoming: true
  track_outgoing: true
  filter_ports: []

# Optional monitoring modules
bandwidth:
  enabled: false
  interfaces:
    - eth0
  interval: 10s

latency:
  enabled: false
  targets:
    - 8.8.8.8
    - 1.1.1.1
  interval: 30s
  timeout: 500ms

dns:
  enabled: false
  interval: 1m

logging:
  level: info
  format: json
```

### Locations (locations.yaml)

```yaml
locations:
  - network: 10.179.64.0/22
    location: IX-M5-SM13
  - network: 10.181.208.0/22
    location: IX-M3-SM10
```

### Roles (roles.yaml)

```yaml
roles:
  - network: 10.179.64.32/32
    role: s3-dwh05
  - network: 10.179.65.31/32
    role: dwh-lb
```

See [Configuration Guide](docs/configuration.md) for all options.

---

## 🔌 API Reference

### Netmon Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/metrics` | GET | Prometheus metrics |
| `/health` | GET | Health check |
| `/ready` | GET | Readiness check |
| `/api/v1/loss/top` | GET | Top lossy IP pairs |
| `/api/v1/discover` | POST | Discover path for IP pair |
| `/api/v1/discover/top` | GET | Top paths with details |

### Conntrack Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/v1/conntrack/connections` | GET | List active connections |
| `/api/v1/conntrack/stats` | GET | Connection statistics |

See [Discovery API Reference](docs/DISCOVERY_API.md) for details.

---

## 📊 Metrics

### Netmon Metrics

```prometheus
# TCP retransmits by connection pair
netmon_tcp_loss_total{
    src_ip="10.179.64.32",
    dst_ip="10.181.208.50",
    src_location="IX-M5-SM13",
    dst_location="IX-M3-SM10",
    src_role="s3-dwh05",
    dst_role="dwh-lb"
}

# Discovery metrics
netmon_discovery_paths_total
netmon_discovery_last_run_seconds
netmon_path_hops{src_ip="...", dst_ip="..."}
netmon_path_rtt_seconds{src_ip="...", dst_ip="..."}
netmon_path_bottleneck_loss_percent{src_ip="...", dst_ip="..."}
```

### Conntrack Metrics

```prometheus
# Connection states
conntrack_connections{state="established", direction="outgoing"}

# Events
conntrack_events_total{event="NEW", direction="outgoing"}
conntrack_events_total{event="ESTABLISHED", direction="outgoing"}
conntrack_events_total{event="CLOSED", direction="outgoing"}

# Handshake duration
conntrack_handshake_duration_seconds{direction="outgoing"}

# Connection duration
conntrack_connection_duration_seconds{direction="outgoing"}

# Bytes transferred
conntrack_bytes_total{direction="outgoing", type="sent"}
conntrack_bytes_total{direction="outgoing", type="received"}
conntrack_bytes_per_connection{direction="outgoing"}
```

### Bandwidth Metrics

```prometheus
# Interface throughput
netmon_bandwidth_bytes_per_sec{
    interface="eth0",
    direction="rx"  # or "tx"
}

# Interface errors
netmon_bandwidth_errors_total{
    interface="eth0",
    type="rx_errors"  # or "tx_errors", "rx_dropped", "tx_dropped"
}
```

### Latency Metrics

```prometheus
# RTT histogram
netmon_latency_seconds_bucket{target="8.8.8.8",le="0.01"}
netmon_latency_seconds_sum{target="8.8.8.8"}
netmon_latency_seconds_count{target="8.8.8.8"}

# Timeout counter
netmon_latency_timeouts_total{target="8.8.8.8"}
```

### DNS Metrics

```prometheus
# DNS query results
netmon_dns_queries_total{
    domain="google.com",
    status="success"  # or "failure"
}

# DNS latency
netmon_dns_latency_seconds{domain="google.com"}
```

---

## 📚 Documentation

| Document | Description |
|----------|-------------|
| [Status & Plan](docs/STATUS_AND_PLAN.md) | Current development status |
| [Discovery API](docs/DISCOVERY_API.md) | API reference for path discovery |
| [Conntrack Guide](docs/CONNTRACK.md) | Connection tracking documentation |
| [Configuration](docs/configuration.md) | Full configuration reference |
| [Deployment](docs/DOCKER_DEPLOYMENT.md) | Docker deployment guide |
| [Architecture](docs/architecture.md) | System architecture details |

---

## 🛠 Development

### Build

```bash
# Build all
make build

# Build specific binary
make build-netmon
make build-conntrack

# Build eBPF programs
make -C bpf all
```

### Test

```bash
# Run all tests
make test

# Run with coverage
make test-coverage

# Run integration tests (requires root)
sudo go test -v ./tests/integration/...
```

### Lint

```bash
make lint
```

### Project Structure

```
network-monitor/
├── cmd/
│   ├── netmon/           # Netmon application
│   └── conntrack/        # Conntrack application
├── internal/
│   ├── collector/        # trace_pipe collector
│   ├── conntrack/        # Connection tracking
│   ├── discovery/        # Path discovery & traceroute
│   ├── metadata/         # Location/Role matching
│   ├── metrics/          # Prometheus exporter
│   ├── config/           # Configuration
│   ├── topology/         # Network topology
│   ├── packetloss/       # Packet loss monitor
│   ├── bandwidth/        # Bandwidth monitor
│   ├── latency/          # Latency monitor
│   └── dns/              # DNS monitor
├── bpf/                  # eBPF programs
├── configs/              # Example configurations
├── dashboards/           # Grafana dashboards
├── docs/                 # Documentation
├── tests/                # Integration tests
└── pkg/                  # Shared packages
```

---

## 📄 License

MIT License — see [LICENSE](LICENSE) for details.

---

## 🤝 Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for contribution guidelines.
