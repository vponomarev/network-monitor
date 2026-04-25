# API Reference

This document describes the public API and metrics exposed by Network Monitor.

## Prometheus Metrics

### Packet Loss Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `netmon_packet_loss_total` | Counter | `interface` | Total packet loss events |
| `netmon_packet_loss_percent` | Gauge | `interface` | Current loss percentage |

Example:
```
netmon_packet_loss_total{interface="eth0"} 15
netmon_packet_loss_percent{interface="eth0"} 0.5
```

### Connection Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `netmon_connections_total` | Counter | `direction`, `protocol` | Total connections |
| `netmon_active_connections` | Gauge | - | Current active connections |

Example:
```
netmon_connections_total{direction="outgoing",protocol="tcp"} 1250
netmon_active_connections 42
```

### Latency Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `netmon_latency_seconds` | Histogram | `target` | RTT distribution |

Example:
```
netmon_latency_seconds_bucket{target="8.8.8.8",le="0.01"} 100
netmon_latency_seconds_bucket{target="8.8.8.8",le="0.05"} 150
netmon_latency_seconds_sum{target="8.8.8.8"} 2.5
netmon_latency_seconds_count{target="8.8.8.8"} 200
```

### Bandwidth Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `netmon_bandwidth_bytes` | Gauge | `interface`, `direction` | Bytes per second |

Example:
```
netmon_bandwidth_bytes{interface="eth0",direction="rx"} 1048576
netmon_bandwidth_bytes{interface="eth0",direction="tx"} 524288
```

### DNS Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `netmon_dns_queries_total` | Counter | `status` | Total DNS queries |
| `netmon_dns_latency_seconds` | Histogram | `server` | DNS latency |

Example:
```
netmon_dns_queries_total{status="success"} 500
netmon_dns_queries_total{status="failure"} 5
netmon_dns_latency_seconds_sum{server="8.8.8.8"} 0.025
```

## Event Types

Events are sent via Go channels and can be consumed by external systems.

### Packet Loss Event

```go
type PacketLossEvent struct {
    Type:      "packet_loss"
    Source:    "packetloss"
    Data: map[string]interface{}{
        "interface":    "eth0",
        "loss_percent": 2.5,
        "threshold":    1.0,
    }
}
```

### Connection Event

```go
type ConnectionEvent struct {
    Type:      "new_connection" | "close_connection"
    Source:    "conntrack"
    Data: map[string]interface{}{
        "source_ip":    "192.168.1.100",
        "source_port":  54321,
        "dest_ip":      "8.8.8.8",
        "dest_port":    443,
        "protocol":     6,
        "direction":    "outgoing",
        "pid":          1234,
        "process_name": "curl",
    }
}
```

### Latency Event

```go
type LatencyEvent struct {
    Type:      "high_latency" | "timeout"
    Source:    "latency"
    Data: map[string]interface{}{
        "target": "8.8.8.8",
        "rtt_ms": 500,
    }
}
```

### DNS Event

```go
type DNSEvent struct {
    Type:      "dns_failure" | "dns_slow"
    Source:    "dns"
    Data: map[string]interface{}{
        "domain":     "example.com",
        "error":      "NXDOMAIN",
        "latency_ms": 600,
    }
}
```

## Go API

### Creating a Monitor

```go
import (
    "github.com/vponomarev/network-monitor/internal/packetloss"
    "github.com/vponomarev/network-monitor/internal/config"
    "go.uber.org/zap"
)

logger, _ := zap.NewProduction()
cfg := config.PacketLossConfig{
    Interfaces: []string{"eth0"},
    ThresholdPercent: 1.0,
}

monitor := packetloss.NewMonitor(cfg, logger)
```

### Running the Monitor

```go
ctx := context.Background()
err := monitor.Run(ctx)
```

### Getting Statistics

```go
total, lost, percent := monitor.GetStats("eth0")
```

### Receiving Events

```go
for event := range monitor.Events() {
    fmt.Printf("Event: %s - %v\n", event.Type, event.Data)
}
```

## HTTP Endpoints

### Metrics Endpoint

```
GET /metrics
```

Returns Prometheus-formatted metrics.

### Health Endpoint

```
GET /health
```

Returns `200 OK` if service is healthy.

Response:
```
OK
```

## Configuration API

### Load Configuration

```go
import "github.com/vponomarev/network-monitor/internal/config"

cfg, err := config.Load("/etc/netmon/config.yaml")
if err != nil {
    // Handle error
}
```

### Default Configuration

```go
cfg := config.DefaultConfig()
```

## Error Handling

All API functions return errors that should be handled:

```go
if err := monitor.Run(ctx); err != nil {
    if errors.Is(err, context.Canceled) {
        // Graceful shutdown
    } else {
        // Actual error
        log.Fatal(err)
    }
}
```

## Rate Limiting

Events are rate-limited per module:

| Module | Rate Limit |
|--------|------------|
| packetloss | 1 alert per `alert_interval` |
| conntrack | No limit (ring buffer backpressure) |
| latency | 1 alert per measurement |
| dns | 1 alert per query |
