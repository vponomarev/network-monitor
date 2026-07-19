# HTTP and Metrics Reference

The HTTP surface depends on the selected application. Both services use the
address from `global.metrics_addr` and `global.metrics_port`.

## Common endpoints

| Method | Endpoint | Authentication | Description |
|---|---|---|---|
| GET | `/health` | Never | Process liveness |
| GET | `/ready` | Never | Collector readiness; 503 before eBPF startup |
| GET | `/metrics` | Optional bearer | Prometheus exposition |
| GET | `/api/v1/version` | Optional bearer | Running binary version and build identity |

Query the process that is actually serving traffic:

```bash
curl --fail http://127.0.0.1:9876/api/v1/version
```

```json
{
  "service": "netmon",
  "version": "v2.6.0",
  "git_commit": "abc1234",
  "build_time": "2026-07-19T00:00:00Z",
  "go_version": "go1.24.4",
  "goos": "linux",
  "goarch": "amd64"
}
```

The same identity is exported for inventory queries as
`netmon_build_info{version,git_commit,build_time,go_version} 1` or
`conntrack_build_info{version,git_commit,build_time,go_version} 1`.

## Netmon API

When the corresponding modules are enabled, `netmon` also exposes:

| Method | Endpoint | Description |
|---|---|---|
| POST | `/api/v1/discover` | Discover a path for `src_ip` and `dst_ip` |
| GET | `/api/v1/discover/top` | Discover current top-loss pairs |
| GET | `/api/v1/loss/top?limit=N` | Return top observed loss pairs |
| GET | `/api/v1/metadata/status` | Metadata source and polling status |
| POST | `/api/v1/metadata/refresh` | Immediately fetch and apply HTTP metadata sources |
| GET | `/api/v1/metadata/unknown` | Bounded inventory of IPs with unresolved role, location, or VRF |
| GET | `/metrics/metadata/unknown` | Opt-in Prometheus per-IP unknown metadata series |
| POST | `/api/v1/config/reload` | Reload metadata and topology files from the current config |
| GET | `/api/v1/conntrack/connections` | Combined-mode active connections |
| GET | `/api/v1/conntrack/stats` | Combined-mode connection statistics |

Discovery endpoints are available only when `discovery.traceroute.enabled` is
true. Start an on-demand discovery with:

```bash
curl --fail -X POST -H 'Content-Type: application/json' \
  -d '{"src_ip":"192.0.2.10","dst_ip":"198.51.100.20"}' \
  http://127.0.0.1:9876/api/v1/discover
```

Discover paths for the configured top-loss pairs or inspect the recorded pairs:

```bash
curl --fail http://127.0.0.1:9876/api/v1/discover/top
curl --fail 'http://127.0.0.1:9876/api/v1/loss/top?limit=20'
```

`limit` defaults to `discovery.traceroute.top_n` on `/api/v1/loss/top`.

### Metadata status

```bash
curl --fail http://127.0.0.1:9876/api/v1/metadata/status
```

The response always includes the configured local `file_path`. `enabled`
indicates whether HTTP auto-update is active. When it is configured, `http_url`
contains the update URL; polling timestamps and hash remain empty until the
first successful update.

```json
{
  "sources": {
    "locations": {
      "file_path": "/etc/netmon/locations.yaml",
      "http_url": "https://metadata.example/locations.yaml",
      "update_success": true,
      "entries_count": 12,
      "enabled": true
    },
    "roles": {
      "file_path": "/etc/netmon/roles.yaml",
      "update_success": false,
      "entries_count": 8,
      "enabled": false
    }
  }
}
```

`last_check` is the most recent HTTP attempt, including a failed one.
`last_update` changes only after data has been validated, written, and reloaded
successfully.

### Force metadata refresh

Trigger all configured HTTP metadata sources without waiting for their polling
timers:

```bash
curl --fail-with-body -X POST \
  -H 'Content-Type: application/json' \
  -d '{}' \
  http://127.0.0.1:9876/api/v1/metadata/refresh
```

An omitted `force` defaults to `true`: even when the remote hash is unchanged,
the local file is rewritten and the in-memory matcher is reloaded. To select
sources or retain normal hash-based behavior:

```bash
curl --fail-with-body -X POST \
  -H 'Content-Type: application/json' \
  -d '{"sources":["roles","locations"],"force":false}' \
  http://127.0.0.1:9876/api/v1/metadata/refresh
```

Each source reports `updated` or `unchanged`. If fetching, validating, writing,
or reloading any source fails, the endpoint returns `500`; the affected source
has status `error` and its `error` field contains the same detailed error that
is written to the service log. Validation happens before the active file is
replaced. Other requested sources are still attempted, so the operation is not
transactional across sources.

```json
{
  "sources": {
    "roles": {
      "status": "error",
      "file_path": "/etc/netmon/roles.yaml",
      "http_url": "https://metadata.example/roles.yaml",
      "error": "validating roles metadata: role entry 0: network or networks is required"
    }
  }
}
```

### Unknown metadata inventory

The inventory remains available when `src_ip` and `dst_ip` are excluded from
`netmon_tcp_loss_total`:

```bash
curl --fail 'http://127.0.0.1:9876/api/v1/metadata/unknown?limit=1000'
curl --fail 'http://127.0.0.1:9876/api/v1/metadata/unknown?attribute=role&limit=100'
```

`attribute` is optional and accepts `role`, `location`, or `vrf`. `limit`
defaults to 1000 and cannot exceed `metadata.unknown.max_ips`.

```json
{
  "entries": [
    {
      "ip": "10.181.212.190",
      "missing": ["role"],
      "directions": ["dst"],
      "first_seen": "2026-07-19T18:00:00Z",
      "last_seen": "2026-07-19T18:05:00Z",
      "event_count": 17
    }
  ],
  "total": 1,
  "limit": 1000,
  "truncated": false
}
```

Prometheus per-IP diagnostics use a separate endpoint and are not included in
the default `/metrics` scrape:

```bash
curl --fail http://127.0.0.1:9876/metrics/metadata/unknown
```

```prometheus
netmon_metadata_unknown_ip{ip="10.181.212.190",attribute="role"} 1
```

Both endpoints use the same bearer authentication as `/metrics` and other
`/api/*` routes. Scraping the per-IP endpoint creates one Prometheus series per
IP and missing attribute; configure it as a separate opt-in scrape job.

### Configuration reload

`POST /api/v1/config/reload` is the HTTP equivalent of sending `SIGHUP`:

```bash
curl --fail -X POST http://127.0.0.1:9876/api/v1/config/reload
```

Success returns `200` with `{"status":"reloaded"}`. Invalid configuration or
a failed metadata/topology reload returns `500` with a JSON error. Reloads are
serialized. The operation re-reads the config file and applies locations,
roles, and enabled topology data. Changes to listeners, authentication,
collectors, pollers, logging, and metric schema still require a process restart.
The reload is not transactional: all reloadable sources are attempted, and a
partial failure returns `500` while successfully loaded sources remain active.
Protect this mutating endpoint with `global.auth_token` or bind the service to a
trusted interface.

### Conntrack queries

```bash
curl --fail \
  'http://127.0.0.1:9876/api/v1/conntrack/connections?limit=100&state=established&direction=outgoing'
curl --fail http://127.0.0.1:9876/api/v1/conntrack/stats
```

Connection list filters are `limit` (default `100`), `state`, and `direction`.

## Standalone conntrack

The standalone service exposes `/health`, `/ready`, `/metrics`, and
`/api/v1/version`. The `/api/v1/conntrack/*` handlers above belong to netmon's
combined-mode wiring and are not a standalone delivery contract.

## Authentication

When `global.auth_token` or `NETMON_AUTH_TOKEN` is set, send:

```text
Authorization: Bearer <token>
```

For example:

```bash
curl --fail -X POST \
  -H 'Authorization: Bearer <token>' \
  http://127.0.0.1:9876/api/v1/config/reload
```

Netmon protects `/metrics` and `/api/*`. Standalone conntrack protects
`/metrics` and `/api/v1/version`; health probes remain unauthenticated.

## Principal metrics

Netmon exports `netmon_tcp_loss_total`, collector health/drop counters,
cardinality gauges/counters, bounded `netmon_metadata_unknown_*` aggregates,
metadata polling metrics, and `netmon_build_info`.
Standalone conntrack exports `conntrack_build_info` and:
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
