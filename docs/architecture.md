# Architecture

Network Monitor ships two production-qualified Linux `amd64` applications and
one experimental legacy binary.

## Production data paths

```text
tcp_retransmit_skb tracepoint
        │
        ▼
 tcploss.bpf.o ── ring buffer ── netmon ── Prometheus / discovery API

inet_sock_set_state + accept + close
        │
        ▼
conntrack.bpf.o ─ ring buffer ─ conntrack ─ Prometheus + syslog
```

Both eBPF objects are embedded into their Go binaries. A file supplied through
an explicit CLI override is intended for development and diagnosis. Production
startup fails when eBPF cannot be loaded; it does not silently simulate events.

## Applications

### `cmd/netmon`

The TCP-loss daemon loads `internal/losscollector`, enriches retransmit tuples
with metadata, controls Prometheus cardinality, and optionally runs traceroute
discovery. Its HTTP surface includes health, readiness, metrics, discovery,
metadata status, and—when enabled in the combined configuration—legacy
conntrack API handlers.

### `cmd/conntrack`

The standalone lifecycle service loads `internal/conntrack`, correlates IPv4
TCP state changes, exports lifecycle/drop metrics, and writes syslog records.
Its supported HTTP surface is `/health`, `/ready`, and `/metrics`. It is shipped
as a separate systemd service in v2.2.0.

### `cmd/pktloss`

This legacy `trace_pipe` prototype is experimental and is not included in
production releases.

## Package boundaries

| Path | Responsibility |
|---|---|
| `internal/losscollector` | eBPF TCP retransmit reader |
| `internal/conntrack` | connection tracker, state machine, metrics, syslog |
| `internal/metrics` | TCP-loss exporter and cardinality control |
| `internal/metadata` | location, role, topology lookup and polling |
| `internal/discovery` | traceroute and top-loss discovery API |
| `internal/health` | liveness/readiness state |
| `internal/config` | YAML configuration and validation |
| `pkg/embedded` | embedded eBPF, config, and systemd resources |
| `bpf/` | eBPF C sources and build inputs |

## Operational boundaries

- Supported release architecture: Linux `amd64` only.
- Qualified kernels: 5.15, 6.1, 6.8, and 6.12 with BTF.
- Conntrack is IPv4-only in the current production scope.
- Alerts, long-term storage, and dashboards are external consumers.
- Retention and hard limits for orphaned conntrack state are active P0 work.

See [`STATUS_AND_PLAN.md`](STATUS_AND_PLAN.md) for current priorities.
