# Discovery API Reference

## Overview

The Discovery API provides endpoints for network path discovery and loss analysis. It uses traceroute to discover network paths and identify bottlenecks.

**Base URL:** `http://localhost:9876/api/v1`

---

## Endpoints

### 1. Get Top Lossy IP Pairs

Returns the top N IP pairs with the highest packet loss.

**Request:**
```http
GET /api/v1/loss/top?limit=10
```

**Parameters:**
| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `limit` | int | 10 | Maximum number of pairs to return (1-100) |

**Response:**
```json
[
  {
    "src_ip": "10.181.208.50",
    "dst_ip": "10.179.64.39",
    "loss_count": 1301,
    "first_seen": "2024-01-15T10:00:00Z",
    "last_seen": "2024-01-15T10:30:00Z"
  },
  {
    "src_ip": "10.181.208.51",
    "dst_ip": "10.179.64.40",
    "loss_count": 856,
    "first_seen": "2024-01-15T10:05:00Z",
    "last_seen": "2024-01-15T10:30:00Z"
  }
]
```

**Status Codes:**
- `200 OK` - Success
- `400 Bad Request` - Invalid limit parameter

---

### 2. Discover Path (On-Demand)

Discovers the network path between two IP addresses using traceroute.

**Request:**
```http
POST /api/v1/discover
Content-Type: application/json

{
  "src_ip": "10.181.208.50",
  "dst_ip": "10.179.64.39"
}
```

**Body Parameters:**
| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `src_ip` | string | Yes | Source IP address |
| `dst_ip` | string | Yes | Destination IP address |

**Response:**
```json
{
  "path_id": "path-10.181.208.50-10.179.64.39",
  "src_ip": "10.181.208.50",
  "dst_ip": "10.179.64.39",
  "hops": [
    {
      "ttl": 1,
      "ip": "10.181.208.1",
      "hostname": "leaf-01.local",
      "rtt_seconds": 0.0005,
      "loss_percent": 0
    },
    {
      "ttl": 2,
      "ip": "10.0.0.1",
      "hostname": "spine-01.local",
      "rtt_seconds": 0.0012,
      "loss_percent": 0
    },
    {
      "ttl": 3,
      "ip": "10.179.64.1",
      "hostname": "leaf-03.local",
      "rtt_seconds": 0.0028,
      "loss_percent": 2.5
    }
  ],
  "bottleneck": {
    "hop_ip": "10.179.64.1",
    "hop_ttl": 3,
    "loss_percent": 2.5
  },
  "total_loss_percent": 2.5,
  "avg_rtt_seconds": 0.0015,
  "total_hops": 3,
  "discovered_at": "2024-01-15T10:30:00Z"
}
```

**Response Fields:**
| Field | Type | Description |
|-------|------|-------------|
| `path_id` | string | Unique identifier for this path |
| `src_ip` | string | Source IP address |
| `dst_ip` | string | Destination IP address |
| `hops` | array | List of hops in the path |
| `bottleneck` | object | Hop with highest loss |
| `total_loss_percent` | float | Total packet loss percentage |
| `avg_rtt_seconds` | float | Average round-trip time |
| `total_hops` | int | Number of hops discovered |
| `discovered_at` | string | Timestamp of discovery |

**Status Codes:**
- `200 OK` - Success
- `400 Bad Request` - Invalid IP addresses
- `500 Internal Server Error` - Traceroute failed

---

### 3. Get Top Paths

Returns detailed path information for the top N lossy IP pairs.

**Request:**
```http
GET /api/v1/discover/top?limit=5
```

**Parameters:**
| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `limit` | int | 10 | Maximum number of paths to return (1-20) |

**Response:**
```json
{
  "paths": [
    {
      "path_id": "path-10.181.208.50-10.179.64.39",
      "src_ip": "10.181.208.50",
      "dst_ip": "10.179.64.39",
      "loss_count": 1301,
      "hops": [...],
      "bottleneck": {...}
    }
  ],
  "generated_at": "2024-01-15T10:30:00Z"
}
```

**Status Codes:**
- `200 OK` - Success
- `400 Bad Request` - Invalid limit parameter

---

## Traceroute Configuration

The traceroute behavior can be configured in `config.yaml`:

```yaml
discovery:
  traceroute:
    enabled: true
    top_n: 10
    mode: both  # both | top_loss | on_demand | periodic
    interval: 5m
    
    # Traceroute-specific settings
    protocol: icmp        # icmp | udp | tcp
    max_hops: 30          # Maximum TTL
    timeout: 3s           # Per-probe timeout
    probes_per_hop: 3     # Probes per TTL value
    dst_port: 33434       # Destination port for UDP/TCP
    tcp_flags: S          # TCP flags for TCP traceroute
```

### Protocol Selection

| Protocol | Use Case | Firewall Traversal |
|----------|----------|-------------------|
| **ICMP** | Internal networks | Poor |
| **UDP** | Legacy compatibility | Fair |
| **TCP** | **Production (recommended)** | **Excellent** |

TCP traceroute uses SYN packets that look like normal connection attempts, making it ideal for traversing firewalls.

---

## Metrics

The Discovery service exports the following Prometheus metrics:

```prometheus
# Total number of discovered paths
netmon_discovery_paths_total

# Timestamp of last discovery run
netmon_discovery_last_run_seconds

# Path hop count
netmon_path_hops{src_ip="...", dst_ip="..."}

# Path RTT in seconds
netmon_path_rtt_seconds{src_ip="...", dst_ip="..."}

# Bottleneck loss percentage
netmon_path_bottleneck_loss_percent{src_ip="...", dst_ip="..."}
```

---

## Error Responses

### 400 Bad Request
```json
{
  "error": "invalid src_ip format"
}
```

### 500 Internal Server Error
```json
{
  "error": "traceroute failed: operation not permitted"
}
```

---

## Examples

### cURL Examples

```bash
# Get top lossy pairs
curl http://localhost:9876/api/v1/loss/top?limit=5

# Discover path
curl -X POST http://localhost:9876/api/v1/discover \
  -H "Content-Type: application/json" \
  -d '{"src_ip": "10.181.208.50", "dst_ip": "10.179.64.39"}'

# Get top paths
curl http://localhost:9876/api/v1/discover/top?limit=5
```

### Python Example

```python
import requests

# Discover path
response = requests.post(
    'http://localhost:9876/api/v1/discover',
    json={'src_ip': '10.181.208.50', 'dst_ip': '10.179.64.39'}
)

if response.status_code == 200:
    path = response.json()
    print(f"Path has {path['total_hops']} hops")
    print(f"Bottleneck: {path['bottleneck']['hop_ip']}")
    print(f"Total loss: {path['total_loss_percent']}%")
```

### Go Example

```go
package main

import (
    "bytes"
    "encoding/json"
    "net/http"
)

type DiscoverRequest struct {
    SrcIP string `json:"src_ip"`
    DstIP string `json:"dst_ip"`
}

type DiscoverResponse struct {
    PathID          string  `json:"path_id"`
    SrcIP           string  `json:"src_ip"`
    DstIP           string  `json:"dst_ip"`
    TotalHops       int     `json:"total_hops"`
    TotalLossPercent float64 `json:"total_loss_percent"`
}

func discoverPath(srcIP, dstIP string) (*DiscoverResponse, error) {
    req := &DiscoverRequest{SrcIP: srcIP, DstIP: dstIP}
    body, _ := json.Marshal(req)
    
    resp, err := http.Post(
        "http://localhost:9876/api/v1/discover",
        "application/json",
        bytes.NewReader(body),
    )
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    
    var result DiscoverResponse
    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return nil, err
    }
    
    return &result, nil
}
```

---

## Best Practices

1. **Rate Limiting**: Limit on-demand discovery requests to avoid network congestion
2. **Caching**: Use cached paths when possible (TTL: 10 minutes by default)
3. **Error Handling**: Always check for error responses and handle gracefully
4. **Monitoring**: Monitor `netmon_discovery_last_run_seconds` to ensure discovery is running

---

*API Version: 1.0*
*Last Updated: 2026-04-27*
