# Architecture

This document describes the architecture of the Network Monitor system.

## Overview

Network Monitor is a modular Linux network monitoring suite consisting of several components:

```
┌─────────────────────────────────────────────────────────────────┐
│                        CLI Applications                          │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐              │
│  │   netmon    │  │   pktloss   │  │  conntrack  │              │
│  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘              │
└─────────┼────────────────┼────────────────┼─────────────────────┘
          │                │                │
┌─────────┴────────────────┴────────────────┴─────────────────────┐
│                      Internal Modules                            │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐              │
│  │ packetloss  │  │  conntrack  │  │   latency   │              │
│  │ (trace_pipe)│  │   (eBPF)    │  │  (ICMP/UDP) │              │
│  └─────────────┘  └─────────────┘  └─────────────┘              │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐              │
│  │  bandwidth  │  │     dns     │  │   metrics   │              │
│  │ (/proc/net) │  │  (resolver) │  │ (Prometheus)│              │
│  └─────────────┘  └─────────────┘  └─────────────┘              │
└─────────────────────────────────────────────────────────────────┘
          │                │                │
┌─────────┴────────────────┴────────────────┴─────────────────────┐
│                        Linux Kernel                              │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐              │
│  │  trace_pipe │  │    eBPF     │  │  /proc/net  │              │
│  │   (ftrace)  │  │  (ringbuf)  │  │   (stats)   │              │
│  └─────────────┘  └─────────────┘  └─────────────┘              │
└─────────────────────────────────────────────────────────────────┘
```

## Components

### CLI Applications

| Binary | Description |
|--------|-------------|
| `netmon` | Main daemon with all monitoring features |
| `pktloss` | Standalone packet loss monitor |
| `conntrack` | Standalone connection tracker |

### Internal Modules

#### Packet Loss (`internal/packetloss`)
- Reads from `/sys/kernel/tracing/trace_pipe`
- Parses kernel trace output for packet drop events
- Calculates loss percentage over sliding window
- Sends alerts when threshold exceeded

#### Connection Tracking (`internal/conntrack`)
- Uses eBPF programs attached to kernel networking hooks
- Tracks incoming/outgoing TCP/UDP connections
- Reports via eBPF ring buffer
- Includes process information (PID, comm)

#### Latency (`internal/latency`)
- Sends ICMP ping or UDP probes to targets
- Measures round-trip time (RTT)
- Detects high latency conditions

#### Bandwidth (`internal/bandwidth`)
- Reads `/proc/net/dev` for interface statistics
- Calculates bytes/packets per second
- Tracks RX/TX rates per interface

#### DNS (`internal/dns`)
- Monitors DNS resolution performance
- Detects failures and slow responses
- Tracks query latency to various resolvers

#### Metrics (`internal/metrics`)
- Exports Prometheus metrics
- HTTP server on configurable port
- Pre-defined metrics for all modules

## Data Flow

```
┌──────────────┐     ┌──────────────┐     ┌──────────────┐
│   Kernel     │────▶│   Module     │────▶│   Metrics    │
│   (source)   │     │  (process)   │     │  (Prometheus)│
└──────────────┘     └──────────────┘     └──────────────┘
                            │
                            ▼
                     ┌──────────────┐
                     │    Events    │
                     │  (alerting)  │
                     └──────────────┘
```

## eBPF Architecture

The connection tracker uses eBPF programs:

1. **kprobe/tcp_connect** - Triggers on outgoing TCP connections
2. **kprobe/tcp_v4_accept** - Triggers on incoming TCP connections  
3. **kprobe/tcp_close** - Triggers on connection close

Events are sent to userspace via eBPF ring buffer for efficient processing.

## Configuration

Configuration is loaded from YAML file (default: `/etc/netmon/config.yaml`):

```yaml
monitoring:
  packet_loss:
    enabled: true
    interfaces: [eth0]
    threshold_percent: 1.0
  connections:
    enabled: true
    track_incoming: true
    track_outgoing: true
metrics:
  prometheus:
    enabled: true
    port: 9090
logging:
  level: info
  format: json
```

## Security Considerations

- eBPF programs require `CAP_BPF`, `CAP_NET_ADMIN` capabilities
- trace_pipe access requires `CAP_SYS_ADMIN` or proper tracefs mount
- Running as root is recommended for full functionality
- Systemd service files include security hardening options
