# Network Monitor

[![CI](https://github.com/vponomarev/network-monitor/actions/workflows/ci.yml/badge.svg)](https://github.com/vponomarev/network-monitor/actions/workflows/ci.yml)
[![Release](https://github.com/vponomarev/network-monitor/actions/workflows/release.yml/badge.svg)](https://github.com/vponomarev/network-monitor/actions/workflows/release.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/vponomarev/network-monitor)](https://goreportcard.com/report/github.com/vponomarev/network-monitor)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

Linux network monitoring suite for tracking TCP packet loss using `/sys/kernel/tracing/trace_pipe` with path discovery and traceroute capabilities.

## Features

- **TCP Retransmit Tracking** - Real-time monitoring via trace_pipe
- **Path Discovery** - Automatic network path discovery with traceroute
- **Traceroute Support** - ICMP, UDP, and TCP traceroute (firewall-friendly)
- **Location/Role Mapping** - Best-match IP to location/role lookup
- **Prometheus Metrics** - Export to Prometheus with rich labels
- **HTTP API** - RESTful API for path discovery and loss analysis
- **Grafana Dashboard** - Ready-to-use dashboard included
- **SIGHUP Reload** - Reload configuration without restart

## Quick Start

### Prerequisites

- Linux kernel with tracefs support
- Go 1.21+ (for building)
- Root access (for trace_pipe)
- Docker (optional, for containerized deployment)

### Installation

#### Option 1: Build from Source

```bash
# Mount tracefs (if not already mounted)
sudo mount -t tracefs none /sys/kernel/tracing

# Build
make build

# Copy example configs
cp configs/*.yaml .
```

#### Option 2: Docker (Recommended for Production)

```bash
# Build image
docker build -t netmon:latest .

# Run with required capabilities
docker run -d \
  --name netmon \
  --network host \
  --cap-add CAP_SYS_ADMIN \
  --cap-add CAP_NET_RAW \
  -v /sys/kernel/tracing:/sys/kernel/tracing:ro \
  -v $(pwd)/config.yaml:/etc/netmon/config.yaml:ro \
  netmon:latest

# Or use Docker Compose
docker-compose up -d
```

#### Option 3: Kubernetes

```bash
# Deploy DaemonSet
kubectl apply -f k8s/daemonset.yaml
kubectl apply -f k8s/configmap.yaml
```

#### Option 4: Systemd (Production Linux)

```bash
# One-line install
curl -sL https://github.com/vponomarev/network-monitor/releases/latest/download/install.sh | sudo bash

# Or manual installation
make build
sudo make install

# Manage service
sudo systemctl status netmon
sudo journalctl -u netmon -f
```

### Configuration

Edit `config.yaml`:

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
```

Edit `locations.yaml` and `roles.yaml` with your IP mappings.

### Run

```bash
# Run with sudo (required for trace_pipe access)
sudo ./bin/netmon

# Or with custom config
sudo ./bin/netmon --config /path/to/config.yaml
```

### Verify

```bash
# Check metrics
curl http://localhost:9876/metrics

# Check health
curl http://localhost:9876/health

# Get top lossy pairs (Discovery API)
curl http://localhost:9876/api/v1/loss/top?limit=5

# Discover path for specific pair
curl -X POST http://localhost:9876/api/v1/discover \
  -H "Content-Type: application/json" \
  -d '{"src_ip": "10.181.208.50", "dst_ip": "10.179.64.39"}'
```

## HTTP API

### Discovery Endpoints

#### Get Top Lossy Pairs

```bash
GET /api/v1/loss/top?limit=10
```

**Response:**
```json
[
  {
    "src_ip": "10.181.208.50",
    "dst_ip": "10.179.64.39",
    "loss_count": 1301,
    "first_seen": "2024-01-15T10:00:00Z",
    "last_seen": "2024-01-15T10:30:00Z"
  }
]
```

#### Discover Path

```bash
POST /api/v1/discover
Content-Type: application/json

{
  "src_ip": "10.181.208.50",
  "dst_ip": "10.179.64.39"
}
```

**Response:**
```json
{
  "path_id": "path-10.181.208.50-10.179.64.39",
  "src_ip": "10.181.208.50",
  "dst_ip": "10.179.64.39",
  "hops": [
    {"ttl": 1, "ip": "10.181.208.1", "rtt": "0.5ms", "hostname": "gw1.local"},
    {"ttl": 2, "ip": "10.181.208.254", "rtt": "1.2ms"},
    {"ttl": 3, "ip": "10.179.64.1", "rtt": "2.8ms"}
  ],
  "bottleneck": {
    "hop_ip": "10.179.64.1",
    "hop_ttl": 3,
    "loss_percent": 15.5
  },
  "total_loss": 10.5,
  "avg_rtt": "1.5ms"
}
```

#### Get Top Paths

```bash
GET /api/v1/discover/top
```

Returns detailed path information for top N lossy IP pairs.

## Traceroute Modes

Configure traceroute protocol in `config.yaml`:

```yaml
discovery:
  traceroute:
    enabled: true
    protocol: tcp        # icmp | udp | tcp
    dst_port: 443        # For TCP/UDP traceroute
    tcp_flags: S         # SYN flag for TCP traceroute
```

### Protocol Selection

| Protocol | Use Case | Firewall Traversal |
|----------|----------|-------------------|
| **ICMP** | Internal networks | Poor |
| **UDP** | Legacy compatibility | Fair |
| **TCP** | **Production (recommended)** | **Excellent** |

TCP traceroute uses SYN packets that look like normal connection attempts, making it ideal for traversing firewalls.

## Metrics

### Main Metric

```prometheus
# HELP netmon_tcp_loss_total Total number of TCP retransmissions by connection pair
# TYPE netmon_tcp_loss_total counter
netmon_tcp_loss_total{
    src_ip="192.168.1.10",
    dst_ip="192.168.1.20",
    src_location="IX-M5-SM13",
    dst_location="IX-M3-SM10",
    src_role="s3-dwh05",
    dst_role="dwh-lb",
    src_network="10.179.64.0/22",
    dst_network="10.181.208.0/22"
} 15
```

### Labels

| Label | Description |
|-------|-------------|
| `src_ip` | Source IP address |
| `dst_ip` | Destination IP address |
| `src_location` | Source location (from locations.yaml) |
| `dst_location` | Destination location |
| `src_role` | Source role (from roles.yaml) |
| `dst_role` | Destination role |
| `src_network` | Source /24 network |
| `dst_network` | Destination /24 network |

## Grafana Dashboard

Import the included dashboard:

```bash
# Import via Grafana UI
# Dashboard → Import → Upload JSON file
# Select: dashboards/tcp-loss-analysis.json
```

Or via API:

```bash
curl -X POST http://grafana:3000/api/dashboards/db \
  -H "Content-Type: application/json" \
  -d @dashboards/tcp-loss-analysis.json
```

## Configuration Reload

Send SIGHUP to reload configuration without restart:

```bash
# Get PID
pidof netmon

# Send SIGHUP
kill -HUP <pid>
```

## Development

### Build

```bash
make build
```

### Test

```bash
make test
```

### Lint

```bash
make lint
```

## Project Structure

```
network-monitor/
├── cmd/netmon/           # Main application
├── internal/
│   ├── collector/        # trace_pipe reader
│   ├── metadata/         # Location/Role matching
│   ├── metrics/          # Prometheus exporter
│   ├── discovery/        # Path discovery & traceroute
│   │   ├── api.go       # Discovery service
│   │   ├── path.go      # Path model
│   │   ├── cache.go     # Path cache
│   │   ├── top_loss.go  # Loss tracker
│   │   └── traceroute_linux.go  # ICMP/UDP/TCP traceroute
│   └── config/          # Configuration
├── configs/              # Example configs
├── dashboards/           # Grafana dashboards
├── scripts/              # Utility scripts
│   └── collect_trace_data.sh  # Collect trace_pipe samples
├── testdata/             # Test data files
└── docs/                 # Documentation
    ├── PHASE2B_TRACEROUTE.md    # Traceroute implementation
    ├── PHASE2C_INTEGRATION.md   # Integration guide
    ├── TCP_TRACEROUTE.md        # TCP traceroute guide
    ├── REAL_DATA_ANALYSIS.md    # Production data analysis
    └── TEST_COVERAGE.md         # Test coverage report
```

## Migration from Python Version

If you're migrating from the Python TCP loss exporter:

1. Copy your `locations.csv` and `roles.csv` format to YAML
2. Use the same best-match logic (most specific prefix wins)
3. Metric name changed from `tcp_retransmits_total` to `netmon_tcp_loss_total`
4. Update Prometheus scrape config to new port (default: 9876)

## License

MIT License
