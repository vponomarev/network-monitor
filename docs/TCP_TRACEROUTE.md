# TCP Traceroute Implementation

## Overview

TCP traceroute uses TCP SYN packets instead of ICMP or UDP, making it ideal for traversing firewalls that block traditional traceroute protocols.

---

## Why TCP Traceroute?

### Problem
- Many production firewalls block ICMP Echo Request (Type 8)
- UDP traceroute (port 33434+) often filtered
- Only allowed traffic: TCP to specific ports (80, 443, etc.)

### Solution
TCP traceroute sends TCP SYN packets that look like normal connection attempts, passing through firewalls undetected.

---

## How It Works

### Traditional Traceroute vs TCP Traceroute

| Method | Packet Type | Firewall Treatment |
|--------|-------------|-------------------|
| ICMP | Echo Request | ❌ Often blocked |
| UDP | High ports (33434+) | ❌ Often filtered |
| **TCP** | **SYN to port 80/443** | ✅ **Looks like HTTP/HTTPS** |

### TCP Traceroute Flow

```
Sender                    Router 1                 Router 2              Destination
  |                          |                        |                      |
  |--TTL=1, SYN:80--------->|                        |                      |
  |<--ICMP Time Exceeded----|                        |                      |
  |                          |                        |                      |
  |--TTL=2, SYN:80--------->|--TTL=1, SYN:80------->|                        |
  |                          |<--ICMP Time Exceeded--|                      |
  |                          |                        |                      |
  |--TTL=3, SYN:80--------->|--TTL=2, SYN:80------->|--TTL=1, SYN:80------->|
  |                          |                        |<--SYN-ACK------------|
  |                          |                        |  (Connection reached)|
```

### Response Types

| Response | Meaning |
|----------|---------|
| **ICMP Time Exceeded (Type 11)** | Intermediate hop |
| **TCP SYN-ACK** | Destination reached (port open) |
| **TCP RST** | Destination reached (port closed) |
| **No response** | Packet dropped (timeout) |

---

## Configuration

### YAML Configuration

```yaml
discovery:
  traceroute:
    protocol: tcp          # Use TCP traceroute
    dst_port: 443          # Target port (HTTPS)
    tcp_flags: S           # SYN flag
    src_port: 0            # Auto-select source port
    timeout: 3s            # Per-probe timeout
    probes_per_hop: 3      # Probes per TTL
    max_hops: 30           # Maximum TTL
```

### Go API

```go
config := &TracerouteConfig{
    Protocol:     "tcp",
    DstPort:      443,      // HTTPS
    TCPFlags:     "S",      // SYN
    SrcPort:      0,        // Auto
    Timeout:      3 * time.Second,
    ProbesPerHop: 3,
    MaxHops:      30,
}

factory := NewTracerouteFactory(config, logger)
tracerouter, err := factory.Create("tcp")
```

---

## TCP Flags

### Supported Flags

| Flag | Character | Hex | Description |
|------|-----------|-----|-------------|
| SYN  | `S`       | 0x02 | Initiate connection |
| ACK  | `A`       | 0x10 | Acknowledgment |
| FIN  | `F`       | 0x01 | Close connection |
| RST  | `R`       | 0x04 | Reset connection |
| PSH  | `P`       | 0x08 | Push data |
| URG  | `U`       | 0x20 | Urgent data |

### Common Combinations

| Flags | String | Use Case |
|-------|--------|----------|
| SYN | `S` | **Default - Normal connection attempt** |
| SYN-ACK | `SA` | Response from server |
| ACK | `A` | Established connection |
| FIN | `F` | Graceful close |
| RST | `R` | Force reset |

### Example: Different Flag Combinations

```yaml
# Standard SYN (stealthy)
tcp_flags: S

# SYN-ACK (may trigger alerts)
tcp_flags: SA

# ACK only (for established connection simulation)
tcp_flags: A

# FIN (very stealthy, may be blocked)
tcp_flags: F
```

---

## Usage Examples

### Basic TCP Traceroute

```go
ctx := context.Background()
config := &TracerouteConfig{
    Protocol: "tcp",
    DstPort:  443,
    TCPFlags: "S",
}

factory := NewTracerouteFactory(config, logger)
tracerouter, _ := factory.Create("tcp")

result, err := tracerouter.Trace(ctx, "8.8.8.8")
for _, hop := range result.Hops {
    fmt.Printf("%2d: %-15s %s  %v\n", 
        hop.TTL, hop.IP, hop.Hostname, hop.RTT)
}
```

### Traceroute to Multiple Ports

```go
// Test different ports to see which are allowed
ports := []int{80, 443, 8080, 8443}

for _, port := range ports {
    config.DstPort = port
    tracerouter, _ := factory.Create("tcp")
    result, _ := tracerouter.Trace(ctx, dstIP)
    
    fmt.Printf("Port %d: %d hops, completed: %v\n",
        port, len(result.Hops), result.Completed)
}
```

### Concurrent TCP Traceroutes

```go
pool := NewTraceroutePool(factory, 10)

destinations := []string{
    "10.179.64.39",  // High-loss destination from real data
    "10.181.208.80",
    "10.179.64.25",
}

results, err := pool.TraceBatch(ctx, destinations)
```

---

## Port Selection Guide

### Recommended Ports

| Port | Service | Firewall Treatment |
|------|---------|-------------------|
| **80** | HTTP | ✅ Usually open |
| **443** | HTTPS | ✅ Usually open |
| **8080** | HTTP Alt | ⚠️ Sometimes open |
| **8443** | HTTPS Alt | ⚠️ Sometimes open |
| **22** | SSH | ❌ Often restricted |
| **3306** | MySQL | ❌ Usually blocked |

### Best Practices

1. **Use port 443** for production (looks like HTTPS)
2. **Use port 80** as fallback
3. **Avoid well-known restricted ports** (22, 23, 3389)
4. **Match your application ports** for realistic path

---

## Comparison: ICMP vs UDP vs TCP

| Feature | ICMP | UDP | TCP |
|---------|------|-----|-----|
| **Firewall traversal** | Poor | Fair | **Excellent** |
| **Stealth** | Low | Medium | **High** |
| **Accuracy** | Good | Good | **Excellent** |
| **Root required** | Yes | Yes | Yes |
| **Speed** | Fast | Fast | Medium |
| **Detection risk** | Low | Medium | **Very Low** |

### When to Use Each

**ICMP:**
- Internal networks (no firewall)
- Quick diagnostics
- When you control the network

**UDP:**
- Legacy compatibility
- When ICMP is blocked but UDP isn't
- Testing UDP path specifically

**TCP (Recommended):**
- **Production environments**
- **Across firewalls**
- **When only HTTP/HTTPS allowed**
- **Most accurate for TCP applications**

---

## Real-World Example

### Problem: High TCP Retransmits

From our production data:
```
10.181.208.50 -> 10.179.64.39: 1,301 retransmits (18% of total)
```

### Solution: TCP Traceroute Analysis

```bash
# Run TCP traceroute to problematic destination
./bin/netmon-trace --protocol tcp --port 443 10.179.64.39
```

### Expected Output

```
TCP Traceroute to 10.179.64.39:443

 1  10.181.208.1     0.5ms
 2  10.181.208.254   1.2ms
 3  10.179.64.1      2.8ms
 4  10.179.64.10     3.1ms
 5  10.179.64.39     4.5ms  [SYN-ACK] ✓

Bottleneck detected at hop 3:
  - RTT jump: 1.6ms → 2.8ms (75% increase)
  - Device: core-switch-01
```

---

## Security Considerations

### Detection Risk

TCP traceroute with SYN flags is **very stealthy**:
- Looks like normal connection attempt
- No actual connection established (no ACK sent)
- Logs appear as failed connection attempts

### Rate Limiting

```yaml
# Recommended rate limits
probes_per_hop: 3      # Standard
timeout: 3s            # Avoid flooding
max_hops: 30           # Reasonable limit
```

### Firewall Rules

If you need to allow TCP traceroute monitoring:

```bash
# Allow TCP traceroute from monitoring system
iptables -A INPUT -s 10.181.208.50 -p tcp --dport 80 -j ACCEPT
iptables -A INPUT -s 10.181.208.50 -p tcp --dport 443 -j ACCEPT
```

---

## Troubleshooting

### "Permission denied"

```bash
# Solution 1: Run as root
sudo ./bin/netmon

# Solution 2: Set capabilities
sudo setcap cap_net_raw+ep ./bin/netmon
```

### "No response from hops"

**Causes:**
- Firewall dropping all probes
- TTL too low
- Timeout too short

**Solutions:**
```yaml
# Increase timeout
timeout: 5s

# Try different port
dst_port: 80  # Instead of 443

# Use different flags
tcp_flags: A  # ACK instead of SYN
```

### "Incomplete path"

**Cause:** Destination not responding to SYN

**Solutions:**
1. Try different port (80, 443, 8080)
2. Use ICMP or UDP instead
3. Check if destination is actually reachable

---

## Performance

### Resource Usage

| Metric | Value |
|--------|-------|
| Memory per traceroute | ~2 KB |
| CPU (single trace) | <1% |
| Network packets | 90 (30 hops × 3 probes) |
| Time (typical) | 2-5 seconds |

### Optimization Tips

1. **Reduce probes_per_hop** for speed: `probes_per_hop: 2`
2. **Lower max_hops** if destination is close: `max_hops: 15`
3. **Use concurrent pool** for multiple destinations

---

## Files

| File | Purpose |
|------|---------|
| `traceroute_linux.go` | TCP implementation (~300 lines) |
| `traceroute_other.go` | macOS/Windows stub |
| `traceroute_test.go` | Tests (macOS compatible) |

---

## Summary

**TCP Traceroute is recommended for:**
- ✅ Production environments with firewalls
- ✅ Monitoring across security zones
- ✅ Accurate TCP path analysis
- ✅ Stealthy network diagnostics

**Configuration:**
```yaml
protocol: tcp
dst_port: 443
tcp_flags: S
```

**Next:** Integrate with main.go for full network monitoring!
