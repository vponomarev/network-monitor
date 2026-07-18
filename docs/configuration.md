# Configuration Reference

Configuration is YAML. Relative metadata paths are resolved from the directory
containing the main config file. `netmon` uses `config.yaml` by default or the
path supplied by `--config`/`NETMON_CONFIG`; installed conntrack uses
`/etc/conntrack/config.yaml`.

## Production settings

```yaml
global:
  ttl_hours: 3
  metrics_addr: 127.0.0.1
  metrics_port: 9876
  auth_token: ""
  loss_source: ebpf
  trace_pipe_path: /sys/kernel/tracing/trace_pipe

metadata:
  locations:
    path: locations.yaml
  roles:
    path: roles.yaml
  topology:
    path: topology.yaml

discovery:
  traceroute:
    enabled: true
    top_n: 10
    mode: both
    interval: 5m
    protocol: icmp
    dst_port: 33434
    src_port: 0
    tcp_flags: S
    max_hops: 30
    timeout: 3s
    probes_per_hop: 3

topology:
  enabled: false
  path: topology.yaml

metrics:
  name: netmon_tcp_loss_total
  default_labels: [src_ip, dst_ip, src_location, dst_location, src_role, dst_role, src_vrf, dst_vrf]
  optional_labels: [src_network, dst_network]
  cardinality:
    level: role
    max_series: 10000

connections:
  enabled: true
  track_incoming: true
  track_outgoing: true
  filter_ports: []
  event_buffer_size: 10000
  state_ttl: 24h
  cleanup_interval: 1m
  max_tracked_connections: 10240
  max_pending_connections: 16384

logging:
  level: info
  format: json
  output_path: ""
```

`default_labels` and `optional_labels` together form the exact label allowlist
for `netmon_tcp_loss_total`. Optional labels are opt-in; omit the field or use
`optional_labels: []` to export none of them. `metrics.cardinality.level` is an
upper bound: `role` excludes IP/network labels, `network` excludes IP labels,
and `ip` permits all supported labels. For example, with `level: ip`, removing
`src_network` and `dst_network` from `optional_labels` keeps `src_ip`/`dst_ip`
but removes both network labels from Prometheus output.

`loss_source: ebpf` is the production default. `tracepipe` is a legacy debug
fallback. `/health` and `/ready` are always public; `/metrics` and `/api/*`
require `Authorization: Bearer <token>` when `global.auth_token` is non-empty.
`NETMON_AUTH_TOKEN` supplies the token only when it is absent from YAML.

## Metadata polling

Each metadata file may have an optional remote update source:

```yaml
metadata:
  roles:
    path: roles.yaml
    update_source:
      url: https://config.example.com/roles.yaml
      poll_interval: 20m
      timeout: 10s
```

The local file remains the startup source. Successful remote updates are
validated and written atomically.

## Role and location mappings

Both metadata files accept a legacy single `network` or a grouped `networks`
list. These fields are mutually exclusive. Grouped entries are expanded before
longest-prefix matching, so more-specific entries still win.

```yaml
# roles.yaml: roles are commonly assigned per host
roles:
  - role: load-balancer
    networks: [10.10.10.10/32, 10.10.10.11/32]

# locations.yaml: locations commonly contain larger subnets
locations:
  - location: datacenter-a
    vrf: production
    networks:
      - 10.20.0.0/20
      - 10.30.0.0/23
```

Every value must use CIDR notation. Exact duplicate assignments are collapsed;
the same canonical CIDR assigned to different roles or location attributes is
rejected. `GET /api/v1/metadata/status` reports the number of expanded unique
networks, not the number of YAML groups.

## Cardinality

`metrics.cardinality.level` accepts `role` (production default), `network`, or
`ip`. `max_series` is a hard active-series cap; zero means unlimited and is not
recommended. Dropped series increment `netmon_loss_series_dropped_total`.

Optional `bandwidth`, `latency`, `dns`, and `packet_loss` sections exist in the
schema but are outside the current production scope. Start from
[`../configs/config.example.yaml`](../configs/config.example.yaml) or
[`../configs/conntrack.example.yaml`](../configs/conntrack.example.yaml).
