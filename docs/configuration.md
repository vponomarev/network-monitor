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
  unknown:
    enabled: true
    ttl: 3h
    max_ips: 10000

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
fallback. Handshake retransmits in `TCP_SYN_SENT`, `TCP_SYN_RECV`, and
`TCP_NEW_SYN_RECV` are excluded from `netmon_tcp_loss_total`; the metric tracks
retransmissions after connection establishment. `/health` and `/ready` are
always public; `/metrics` and `/api/*`
require `Authorization: Bearer <token>` when `global.auth_token` is non-empty.
`NETMON_AUTH_TOKEN` supplies the token only when it is absent from YAML.

`src_vrf` and `dst_vrf` are safe at `cardinality.level: role`; they do not
require `src_ip` or `dst_ip`. VRF values come from the matching `vrf` field in
`locations.yaml`. Changing metadata values can be applied through config
reload, while changing the metric label allowlist requires a process restart.

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
validated and written atomically. Use `POST /api/v1/metadata/refresh` to fetch
configured HTTP sources immediately. The request defaults to a forced rewrite
and in-memory reload even when the remote hash has not changed; pass
`{"force":false}` for normal hash-based behavior. Invalid remote data is not
written, and the endpoint returns the validation error in its JSON response.

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

The example above assigns `production` to both networks. A loss event is
therefore exported with `src_vrf="production"` or `dst_vrf="production"`
when the corresponding address matches. The current VRF model is CIDR-based.

Every network value must use CIDR notation. Exact duplicate assignments are
collapsed; the same canonical CIDR assigned to different roles or location
attributes is rejected. `GET /api/v1/metadata/status` reports the number of
expanded unique networks, not the number of YAML groups.

## Unknown metadata inventory

When `metadata.unknown.enabled` is true, netmon records IP addresses seen in
loss events whose role, location, or VRF resolves to `unknown`. This inventory
is independent of the `netmon_tcp_loss_total` label allowlist, so operators can
remove `src_ip` and `dst_ip` without losing metadata diagnostics.

`ttl` removes inactive addresses and `max_ips` is a hard in-memory cap. The
main `/metrics` endpoint exposes only bounded aggregates:

```prometheus
netmon_metadata_unknown_ips{attribute="role"} 12
netmon_metadata_unknown_events_total{attribute="location"} 42
netmon_metadata_unknown_observations_dropped_total 0
```

Per-IP series are opt-in on `/metrics/metadata/unknown`; the JSON inventory is
available on `/api/v1/metadata/unknown`. Reconciliation after a metadata reload
immediately removes addresses that have become fully known.

## Cardinality

`metrics.cardinality.level` accepts `role` (production default), `network`, or
`ip`. `max_series` is a hard active-series cap; zero means unlimited and is not
recommended. Dropped series increment `netmon_loss_series_dropped_total`.

Optional `bandwidth`, `latency`, `dns`, and `packet_loss` sections exist in the
schema but are outside the current production scope. Start from
[`../configs/config.example.yaml`](../configs/config.example.yaml) or
[`../configs/conntrack.example.yaml`](../configs/conntrack.example.yaml).
