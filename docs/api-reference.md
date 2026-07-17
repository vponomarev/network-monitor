# HTTP and Metrics Reference

The HTTP surface depends on the selected application. Both services use the
address from `global.metrics_addr` and `global.metrics_port`.

## Common endpoints

| Method | Endpoint | Authentication | Description |
|---|---|---|---|
| GET | `/health` | Never | Process liveness |
| GET | `/ready` | Never | Collector readiness; 503 before eBPF startup |
| GET | `/metrics` | Optional bearer | Prometheus exposition |

## Netmon API

When the corresponding modules are enabled, `netmon` also exposes:

| Method | Endpoint | Description |
|---|---|---|
| POST | `/api/v1/discover` | Discover a path for `src_ip` and `dst_ip` |
| GET | `/api/v1/discover/top` | Discover current top-loss pairs |
| GET | `/api/v1/loss/top?limit=N` | Return top observed loss pairs |
| GET | `/api/v1/metadata/status` | Metadata source and polling status |
| GET | `/api/v1/conntrack/connections` | Combined-mode active connections |
| GET | `/api/v1/conntrack/stats` | Combined-mode connection statistics |

Example discovery request:

```bash
curl --fail -X POST -H 'Content-Type: application/json' \
  -d '{"src_ip":"192.0.2.10","dst_ip":"198.51.100.20"}' \
  http://127.0.0.1:9876/api/v1/discover
```

Connection list filters are `limit`, `state`, and `direction`.

## Standalone conntrack

The v2.2.0 standalone service intentionally exposes only `/health`, `/ready`,
and `/metrics`. The `/api/v1/conntrack/*` handlers above belong to netmon's
combined-mode wiring and are not a standalone delivery contract.

## Authentication

When `global.auth_token` or `NETMON_AUTH_TOKEN` is set, send:

```text
Authorization: Bearer <token>
```

Netmon protects `/metrics` and `/api/*`. Standalone conntrack protects
`/metrics`; health probes remain unauthenticated.

## Principal metrics

Netmon exports `netmon_tcp_loss_total`, collector health/drop counters,
cardinality gauges/counters, and metadata polling metrics. Standalone conntrack
exports:

- `conntrack_connections{state,direction}`;
- `conntrack_events_total{event,direction}`;
- `conntrack_handshake_duration_seconds{direction}`;
- `conntrack_connection_duration_seconds{direction}`;
- `conntrack_bytes_total{direction,type}`;
- `conntrack_bytes_per_connection{direction}`;
- `conntrack_dropped_events_total{reason}`.
- `conntrack_state_entries{layer}` and retention cleanup/eviction/overflow
  counters.

Metric names are a compatibility surface. Changes require a documented schema
version and dashboard migration.
