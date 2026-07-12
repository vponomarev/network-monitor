# Conntrack Test Suite

## Test layers

- Unit tests live in `internal/conntrack/*_test.go`.
- Go integration tests live in `tests/conntrack/integration/` and require Linux
  root privileges.
- Release qualification scripts live in `tests/conntrack/e2e/` and exercise a
  real embedded eBPF program. There is no simulation fallback.

Run unit tests on Linux:

```bash
go test -race ./internal/conntrack/...
```

Run the existing privileged integration package:

```bash
sudo go test -v ./tests/conntrack/integration/...
```

Qualify a ready-made release binary on one host:

```bash
sudo tests/conntrack/e2e/run-host.sh ./conntrack-linux-amd64
```

Or run the same binary on the complete matrix from Windows:

```powershell
.\tests\conntrack\e2e\run-matrix.ps1 `
  -BinaryPath .\dist\conntrack-linux-amd64
```

See [`e2e/README.md`](e2e/README.md) for prerequisites and validation scope.

## Supported matrix

| Host | OS | Kernel |
|---|---|---|
| `192.168.5.217` | Ubuntu 22.04 | `5.15.0-185` |
| `192.168.5.193` | Debian 12 | `6.1.0-45` |
| `192.168.5.99` | Proxmox VE 8 / Debian 12 | `6.8.12-20-pve` |
| `192.168.5.214` | Debian 13 | `6.12.85` |

Tests require Linux amd64, BTF, a mounted/available BPF filesystem, and enough
privilege to load eBPF and attach tracepoints/kprobes. Request SSH credentials
from the user and never store passwords in the repository or documentation.
