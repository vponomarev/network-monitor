# Conntrack Production Guide

`conntrack` is the standalone eBPF service for observing IPv4 TCP connection
lifecycles. It records incoming and outgoing `ESTABLISHED` and `CLOSED` events,
correlates outgoing connections with PID/comm when available, writes structured
syslog messages, and exports Prometheus metrics.

The v2.3.0 Linux `amd64` artifact is production-qualified on kernels 5.15, 6.1,
6.8, and 6.12. Orphaned state is bounded by configurable retention and capacity
limits. IPv6 and ARM are not yet supported. Alerts belong to the external
monitoring stack.

## Runtime architecture

The embedded `conntrack.bpf.o` attaches to `sock/inet_sock_set_state`,
`inet_csk_accept`, and `tcp_close`. Events pass through a bounded ring buffer to
the Go tracker, state machine, Prometheus collector, and syslog writer. Failure
to load or read the production eBPF source is fatal; the service does not fall
back to simulated data.

Conntrack currently runs as a separate binary and systemd unit. It exposes the
following HTTP endpoints:

| Endpoint | Meaning |
|---|---|
| `/health` | Process liveness; returns 200 while HTTP is serving |
| `/ready` | Returns 200 only after eBPF attachment and event reader startup |
| `/metrics` | Prometheus metrics; bearer-protected when `auth_token` is set |
| `/api/v1/version` | Running version, commit, build time, toolchain, and architecture |

Inspect the running build through HTTP or Prometheus:

```bash
curl --fail http://127.0.0.1:9876/api/v1/version
curl --fail http://127.0.0.1:9876/metrics | grep '^conntrack_build_info'
```

When `auth_token` is set, both endpoints require the bearer token. The local
binary can also be inspected without starting the service with
`conntrack --version`.

The older `/api/v1/conntrack/*` handlers are not part of the standalone
production service.

## Installation

Use the raw binary or the conntrack release bundle from GitHub Releases:

```bash
chmod +x conntrack-linux-amd64
sudo ./conntrack-linux-amd64 install
sudo systemctl enable --now conntrack
sudo systemctl status conntrack
```

Installation writes:

- `/usr/local/bin/conntrack`;
- `/etc/conntrack/config.yaml` (only when absent);
- `/etc/systemd/system/conntrack.service`.

`deinstall` removes the managed binary and unit but intentionally preserves the
operator configuration:

```bash
sudo /usr/local/bin/conntrack deinstall
```

Installing over an active service validates the existing config, snapshots the
previous binary/unit under `/var/lib/conntrack/rollback`, atomically replaces
managed files, restarts the service, and waits for readiness. A failed update is
rolled back automatically. Restore the last successful upgrade manually with:

```bash
sudo /usr/local/bin/conntrack rollback
```

## Configuration

Minimal standalone configuration:

```yaml
global:
  metrics_addr: 127.0.0.1
  metrics_port: 9876
  # auth_token: "change-me"

connections:
  enabled: true
  track_incoming: true
  track_outgoing: true
  event_buffer_size: 10000
  state_ttl: 24h
  cleanup_interval: 1m
  max_tracked_connections: 10240
  max_pending_connections: 16384

logging:
  level: info
  format: json
```

The CLI also accepts `--track-incoming`, `--track-outgoing`, `--track-closes`,
`--syn-timeout`, and syslog flags. `--ebpf-prog` is an explicit development or
operator override; production normally uses the embedded object.

Print the embedded example with:

```bash
conntrack show-config
```

## Metrics and logs

Key metrics are:

- `conntrack_connections{state,direction}`;
- `conntrack_events_total{event,direction}`;
- `conntrack_handshake_duration_seconds{direction}`;
- `conntrack_connection_duration_seconds{direction}`;
- `conntrack_dropped_events_total{reason}`.
- `conntrack_state_entries{layer}`;
- `conntrack_state_cleanup_total{reason}`;
- `conntrack_state_evictions_total{layer}`;
- `conntrack_state_overflow_total{layer}`.

Inspect health, metrics, and structured events:

```bash
curl --fail http://127.0.0.1:9876/health
curl --fail http://127.0.0.1:9876/ready
curl --fail http://127.0.0.1:9876/metrics | grep '^conntrack_'
sudo journalctl -u conntrack -f
```

When authentication is enabled:

```bash
curl --fail -H 'Authorization: Bearer change-me' \
  http://127.0.0.1:9876/metrics
```

Syslog lifecycle messages use names such as `CONN_OUT_ESTABLISHED`,
`CONN_IN_ACCEPTED`, and `CONN_CLOSED`, followed by tuple, direction, state,
PID/comm, timing, host, and timestamp fields when available.

## Requirements and qualification

- Linux `amd64` with BTF at `/sys/kernel/btf/vmlinux`;
- root for the supported systemd deployment;
- local syslog service, or reachable TCP/UDP syslog configured by flags;
- kernel from the maintained 5.15/6.1/6.8/6.12 qualification matrix.

Run the release-candidate E2E as root:

```bash
sudo tests/conntrack/e2e/run-host.sh ./conntrack-linux-amd64
```

See [`../tests/conntrack/e2e/README.md`](../tests/conntrack/e2e/README.md) for the
four-host matrix and complete bundle lifecycle test.

## Troubleshooting

```bash
uname -r
test -r /sys/kernel/btf/vmlinux
sudo journalctl -u conntrack -n 100 --no-pager
sudo bpftool prog show
curl -v http://127.0.0.1:9876/ready
```

A 503 from `/ready` means the tracker has not completed eBPF startup. A service
exit is preferable to silently reporting synthetic or incomplete production
data. Check `conntrack_dropped_events_total` for ring-buffer or userspace
backpressure.

## Retention and limits

Version v2.3.0 adds a 24-hour default TTL for orphaned userspace state and
both kernel correlation maps. Sweeps run every minute. Userspace capacity
evicts the oldest snapshot; kernel insertion failures are observable. Operators
can tune TTL, sweep cadence, and both map limits in the `connections` section.
See [`STATUS_AND_PLAN.md`](STATUS_AND_PLAN.md) for current priorities.
