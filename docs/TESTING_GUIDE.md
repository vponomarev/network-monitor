# Testing Guide

Run fast checks locally and privileged eBPF checks on Linux. Go tests are
colocated as `*_test.go`; cross-component scripts live under `tests/`.

## Local checks

```bash
go mod download
make fmt
make vet
make test
```

`make test` runs `go test -v -race ./...`. `make test-coverage` additionally
writes `coverage.out` and `coverage.html`. Use `make check` before a PR when
`golangci-lint` is installed.

Tests must be named `TestXxx`. Prefer table-driven subtests and add coverage
beside changed code. Do not rely on a system `traceroute` being absent or on a
specific developer-machine environment.

## eBPF build and load

On Linux with Clang/LLVM, libbpf headers, BTF, and root access:

```bash
make ebpf-build
cp bpf/conntrack.bpf.o bpf/tcploss.bpf.o pkg/embedded/bpf/
sudo bpftool prog loadall bpf/conntrack.bpf.o /sys/fs/bpf/netmon-test
sudo rm -rf /sys/fs/bpf/netmon-test
```

A Go build or cross-compilation does not establish eBPF runtime compatibility.

## Conntrack E2E

Run a single host qualification:

```bash
sudo tests/conntrack/e2e/run-host.sh ./conntrack-linux-amd64
```

It verifies health/readiness, metrics, incoming and outgoing
`ESTABLISHED`/`CLOSED` lifecycles, tuples, PID/comm, and clean shutdown.

From Windows with PuTTY installed, run the same binary on all hosts:

```powershell
.\tests\conntrack\e2e\run-matrix.ps1 `
  -BinaryPath .\dist\conntrack-linux-amd64
```

| Host | OS | Kernel |
|---|---|---|
| `192.168.5.217` | Ubuntu 22.04 LTS | `5.15.0-185` |
| `192.168.5.193` | Debian 12 | `6.1.0-45` |
| `192.168.5.99` | Proxmox VE 8 / Debian 12 | `6.8.12-20-pve` |
| `192.168.5.214` | Debian 13 | `6.12.85` |

Connect as `root`. Request the current password from the user and never store it
in Git, documentation, scripts, or committed command output.

## Bundle lifecycle

On a systemd host:

```bash
sudo tests/conntrack/e2e/qualify-bundle.sh \
  ./conntrack-v2.3.0-linux-amd64.tar.gz
```

The script backs up and restores existing state while checking install, start,
readiness, metrics, restart, deinstall, and configuration preservation.

Run a 30-minute bounded-state soak profile with:

```bash
sudo CONNTRACK_SOAK_DURATION_SECONDS=1800 \
  tests/conntrack/e2e/run-soak.sh ./conntrack-linux-amd64
```

It enforces readiness, RSS, CPU, drop-delta, and aggregate-state limits. It is
not a deferred high-load benchmark.

## Change-specific requirements

- Go-only change: unit tests, race detector, and `go vet`.
- eBPF event/layout change: rebuild embedded objects, load test, and four-kernel
  runtime matrix.
- Packaging change: bundle lifecycle and repeat installation.
- Retention/limit change: deterministic unit tests for expiry/eviction plus E2E
  proof that readiness and event flow survive cleanup.

Record exact commands, artifact checksum, OS, kernel, and results in the PR.
