# Conntrack E2E Qualification

These scripts qualify the same release-candidate `linux-amd64` binary across
the supported kernel matrix. They require root because conntrack loads and
attaches eBPF programs.

On a Linux host:

```bash
sudo tests/conntrack/e2e/run-host.sh ./conntrack-linux-amd64
```

The host test starts a temporary local HTTP server and conntrack instance. It
checks `/health`, `/ready`, Prometheus metrics, incoming and outgoing
`ESTABLISHED`/`CLOSED` lifecycles, matching tuples, PID/comm correlation, clean
SIGTERM, and reports the OS and kernel. Temporary files are removed; set
`CONNTRACK_E2E_KEEP=1` to retain failure artifacts.

From Windows, run the four-host matrix with PuTTY installed:

```powershell
.\tests\conntrack\e2e\run-matrix.ps1 `
  -BinaryPath .\dist\conntrack-linux-amd64
```

The orchestrator prompts for SSH credentials, pins each known host key, uploads
the binary and host script to a unique `/tmp` directory, and removes them after
the test. Never put the password in the repository or command history. Prefer
SSH keys/Pageant when unattended execution is introduced.

Qualify the complete release bundle on a systemd host:

```bash
sudo tests/conntrack/e2e/qualify-bundle.sh \
  ./conntrack-v2.2.0-linux-amd64.tar.gz
```

This test backs up the existing conntrack binary, config, unit and service
state; verifies install, readiness, metrics, restart, deinstall and config
preservation; and restores the original host state even when a check fails.

Qualify an upgrade over an active baseline installation and explicit rollback:

```bash
sudo tests/conntrack/e2e/qualify-upgrade.sh ./conntrack-linux-amd64
```

The script verifies atomic replacement, automatic restart/readiness, repeated
installation, config preservation, rollback to the exact previous binary/unit,
and restoration of the original host state.

For fault injection on a host where the default metrics port is occupied, use
`qualify-upgrade-isolated.sh <good> <bad-unit-candidate>`. It temporarily moves
the baseline to a dedicated port, runs explicit and automatic rollback checks,
and restores the original host files and service state through a trap.

Run the bounded-state soak profile on a qualification host:

```bash
sudo CONNTRACK_SOAK_DURATION_SECONDS=1800 \
  tests/conntrack/e2e/run-soak.sh ./conntrack-linux-amd64
```

Defaults generate one loopback request per second for 30 minutes and fail when
RSS exceeds 256 MiB, process CPU exceeds 25%, any conntrack drop counter grows,
readiness is lost, or aggregate state exceeds configured limits. Override the
`CONNTRACK_SOAK_*` variables for a longer production qualification. This is a
stability profile, not a stress/load benchmark.
