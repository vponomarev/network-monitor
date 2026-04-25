# Discovery API Documentation

## Overview

The Discovery API provides path discovery and analysis capabilities for identifying network bottlenecks.

## Endpoints

### POST /api/v1/discover

Discover the network path between two IP addresses.

**Request:**
```json
{
    "src_ip": "192.168.1.10",
    "dst_ip": "192.168.2.20"
}
```

**Response:**
```json
{
    "path_id": "path-192.168.1.10-192.168.2.20",
    "src_ip": "192.168.1.10",
    "dst_ip": "192.168.2.20",
    "hops": [
        {
            "ttl": 1,
            "ip": "192.168.1.1",
            "hostname": "leaf-01",
            "rtt": 1000000,
            "lost": false,
            "device": "leaf-dc1-01",
            "layer": "leaf"
        },
        {
            "ttl": 2,
            "ip": "10.0.0.1",
            "hostname": "spine-01",
            "rtt": 2000000,
            "lost": false,
            "device": "spine-dc1-01",
            "layer": "spine"
        }
    ],
    "bottleneck": {
        "hop_ip": "10.0.0.1",
        "hop_ttl": 2,
        "device": "spine-dc1-01",
        "loss_percent": 2.5,
        "rtt_avg": 2000000
    },
    "discovered": "2024-01-15T10:30:00Z",
    "total_loss": 2.5,
    "avg_rtt": "1.5ms"
}
```

### GET /api/v1/discover/top

Discover paths for the top N most lossy connection pairs.

**Response:**
```json
[
    {
        "path_id": "path-192.168.1.10-192.168.2.20",
        "src_ip": "192.168.1.10",
        "dst_ip": "192.168.2.20",
        "hops": [...],
        "bottleneck": {...},
        "total_loss": 5.2,
        "avg_rtt": "2ms"
    }
]
```

### GET /api/v1/loss/top

Get the top N connection pairs by loss count.

**Query Parameters:**
- `limit` (optional): Number of pairs to return (default: 10)

**Response:**
```json
[
    {
        "src_ip": "192.168.1.10",
        "dst_ip": "192.168.2.20",
        "loss_count": 150,
        "last_seen": "2024-01-15T10:30:00Z",
        "loss_rate": 0.5
    }
]
```

## Data Models

### Hop

Represents a single hop in a network path.

| Field | Type | Description |
|-------|------|-------------|
| `ttl` | int | Time To Live (hop number) |
| `ip` | string | IP address of the hop |
| `hostname` | string | Reverse DNS hostname (if available) |
| `rtt` | int | Round-trip time in nanoseconds |
| `lost` | bool | Whether this hop had packet loss |
| `device` | string | Network device ID (if known) |
| `layer` | string | Network layer (leaf, spine, super-spine) |

### Bottleneck

Identifies a network bottleneck.

| Field | Type | Description |
|-------|------|-------------|
| `hop_ip` | string | IP address of the bottleneck hop |
| `hop_ttl` | int | TTL of the bottleneck hop |
| `device` | string | Device ID (if known) |
| `loss_percent` | float | Packet loss percentage |
| `rtt_avg` | int | Average RTT in nanoseconds |

### Path

Complete network path between two hosts.

| Field | Type | Description |
|-------|------|-------------|
| `path_id` | string | Unique identifier |
| `src_ip` | string | Source IP address |
| `dst_ip` | string | Destination IP address |
| `hops` | array | List of hops |
| `bottleneck` | object | Identified bottleneck (if any) |
| `discovered` | timestamp | When the path was discovered |
| `total_loss` | float | Total packet loss percentage |
| `avg_rtt` | string | Average RTT (human-readable) |

## Caching

Paths are cached to avoid excessive traceroute calls:

- **Default TTL**: 10 minutes
- **Max cache size**: 1000 paths
- **Cleanup**: Expired paths removed automatically

## Discovery Modes

| Mode | Description |
|------|-------------|
| `top_loss` | Automatically discover paths for top lossy pairs |
| `on_demand` | Only discover when explicitly requested via API |
| `periodic` | Run discovery at regular intervals |
| `both` | Combine `top_loss` and `on_demand` |

## Examples

### cURL Examples

```bash
# Discover path
curl -X POST http://localhost:9876/api/v1/discover \
  -H "Content-Type: application/json" \
  -d '{"src_ip": "192.168.1.10", "dst_ip": "192.168.2.20"}'

# Get top lossy paths
curl http://localhost:9876/api/v1/discover/top

# Get top loss pairs
curl http://localhost:9876/api/v1/loss/top?limit=20
```

### Go Examples

```go
import "github.com/vponomarev/network-monitor/internal/discovery"

// Create service
service := discovery.DefaultDiscoveryService()

// Discover path
resp, err := service.Discover(ctx, "192.168.1.10", "192.168.2.20")

// Get top lossy pairs
pairs := service.GetLossTracker().GetTopPairs(10)

// Record loss (called by collector)
service.RecordLoss("192.168.1.10", "192.168.2.20")
```

## Integration with Collector

The discovery service integrates with the trace_pipe collector:

```go
// In main.go
collector := collector.NewTracePipeCollector(cfg.Global.TracePipePath, exporter, logger)
exporter.SetDiscoveryService(service)

// When collector detects retransmit:
exporter.RecordRetransmit(srcIP, dstIP)
// This also calls:
service.RecordLoss(srcIP, dstIP)
```

## Error Handling

| HTTP Status | Meaning |
|-------------|---------|
| 200 | Success |
| 400 | Invalid request (missing fields) |
| 405 | Method not allowed |
| 500 | Internal error (traceroute failed) |

## Performance Considerations

- Traceroute operations are expensive (1-5 seconds each)
- Use caching to reduce duplicate lookups
- Limit concurrent discovery operations
- Consider rate limiting for API endpoints
