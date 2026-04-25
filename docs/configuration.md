# Configuration Reference

This document describes all configuration options for Network Monitor.

## Configuration File

The configuration file is YAML format, typically located at `/etc/netmon/config.yaml`.

## Top-Level Sections

```yaml
monitoring:    # Monitoring module settings
metrics:       # Metrics export settings
logging:       # Logging configuration
```

## Monitoring Section

### Packet Loss (`monitoring.packet_loss`)

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | bool | `true` | Enable packet loss monitoring |
| `interfaces` | []string | `["eth0"]` | Network interfaces to monitor |
| `threshold_percent` | float | `1.0` | Alert threshold (%) |
| `window_size` | int | `100` | Sliding window size for calculations |
| `alert_interval` | duration | `1m` | Minimum time between alerts |

Example:
```yaml
monitoring:
  packet_loss:
    enabled: true
    interfaces:
      - eth0
      - eth1
    threshold_percent: 0.5
    window_size: 200
    alert_interval: 30s
```

### Connections (`monitoring.connections`)

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | bool | `true` | Enable connection tracking |
| `track_incoming` | bool | `true` | Track incoming connections |
| `track_outgoing` | bool | `true` | Track outgoing connections |
| `filter_ports` | []int | `[]` | Ports to filter (empty = all) |

Example:
```yaml
monitoring:
  connections:
    enabled: true
    track_incoming: true
    track_outgoing: true
    filter_ports:
      - 22
      - 80
      - 443
```

### Latency (`monitoring.latency`)

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | bool | `true` | Enable latency monitoring |
| `targets` | []string | `["8.8.8.8", "1.1.1.1"]` | Targets to ping |
| `interval` | duration | `10s` | Measurement interval |
| `timeout` | duration | `5s` | Ping timeout |

Example:
```yaml
monitoring:
  latency:
    enabled: true
    targets:
      - 8.8.8.8
      - 1.1.1.1
      - 208.67.222.222
    interval: 5s
    timeout: 3s
```

### Bandwidth (`monitoring.bandwidth`)

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | bool | `true` | Enable bandwidth monitoring |
| `interfaces` | []string | `["eth0"]` | Interfaces to monitor |
| `interval` | duration | `5s` | Collection interval |

Example:
```yaml
monitoring:
  bandwidth:
    enabled: true
    interfaces:
      - eth0
      - eth1
      - docker0
    interval: 1s
```

### DNS (`monitoring.dns`)

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | bool | `true` | Enable DNS monitoring |
| `interfaces` | []string | `["eth0"]` | Interfaces to use |
| `port` | int | `53` | DNS port |
| `interval` | duration | `10s` | Query interval |

Example:
```yaml
monitoring:
  dns:
    enabled: true
    interfaces:
      - eth0
    port: 53
    interval: 30s
```

## Metrics Section

### Prometheus (`metrics.prometheus`)

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | bool | `true` | Enable Prometheus metrics |
| `port` | int | `9090` | HTTP port for metrics |
| `path` | string | `/metrics` | Metrics endpoint path |

Example:
```yaml
metrics:
  prometheus:
    enabled: true
    port: 9090
    path: /metrics
```

## Logging Section

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `level` | string | `info` | Log level (debug, info, warn, error) |
| `format` | string | `json` | Log format (json, console) |
| `output` | string | `` | Log file path (empty = stdout) |

Example:
```yaml
logging:
  level: debug
  format: json
  output: /var/log/netmon/netmon.log
```

## Environment Variables

Configuration can be overridden with environment variables:

```bash
# Config file path
export NETMON_CONFIG=/etc/netmon/config.yaml

# Log level
export NETMON_LOG_LEVEL=debug

# Prometheus port
export NETMON_METRICS_PROMETHEUS_PORT=9091
```

## Full Example

```yaml
monitoring:
  packet_loss:
    enabled: true
    interfaces:
      - eth0
      - eth1
    threshold_percent: 1.0
    window_size: 100
    alert_interval: 1m

  connections:
    enabled: true
    track_incoming: true
    track_outgoing: true
    filter_ports: []

  latency:
    enabled: true
    targets:
      - 8.8.8.8
      - 1.1.1.1
    interval: 10s
    timeout: 5s

  bandwidth:
    enabled: true
    interfaces:
      - eth0
    interval: 5s

  dns:
    enabled: true
    interfaces:
      - eth0
    port: 53
    interval: 30s

metrics:
  prometheus:
    enabled: true
    port: 9090
    path: /metrics

logging:
  level: info
  format: json
  output: /var/log/netmon/netmon.log
```
