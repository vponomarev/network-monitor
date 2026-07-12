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
