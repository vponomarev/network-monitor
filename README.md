# Network Monitor

[![CI](https://github.com/vponomarev/network-monitor/actions/workflows/ci.yml/badge.svg)](https://github.com/vponomarev/network-monitor/actions/workflows/ci.yml)
[![Release](https://github.com/vponomarev/network-monitor/actions/workflows/release.yml/badge.svg)](https://github.com/vponomarev/network-monitor/actions/workflows/release.yml)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

Network Monitor is a Linux `amd64` network-observability suite built around
CO-RE eBPF collectors.

| Application | Purpose | Delivery status |
|---|---|---|
| `netmon` | TCP retransmit/loss monitoring and path discovery | Production-ready |
| `conntrack` | TCP connection lifecycle and process correlation | Production-qualified standalone service |
| `pktloss` | Legacy `trace_pipe` prototype | Experimental; do not use in production |

The v2.3.0 release was qualified on Linux kernels 5.15, 6.1, 6.8, and 6.12.
Only Linux `amd64` artifacts are published. ARM is intentionally unsupported
until an architecture-correct eBPF build can be tested on a real ARM host.

## Quick start

Download a raw binary from the
[latest release](https://github.com/vponomarev/network-monitor/releases/latest),
or use the self-installing conntrack binary:

```bash
wget https://github.com/vponomarev/network-monitor/releases/latest/download/conntrack-linux-amd64
chmod +x conntrack-linux-amd64
sudo ./conntrack-linux-amd64 install
sudo systemctl enable --now conntrack
curl --fail http://127.0.0.1:9876/ready
curl --fail http://127.0.0.1:9876/metrics
```

The installer creates `/etc/conntrack/config.yaml` once and preserves
an existing configuration. Conntrack exposes `/health`, `/ready`, and
`/metrics`; the metrics endpoint supports bearer authentication through
`global.auth_token`.

## Build and test

Go 1.21+ is required. Building eBPF objects additionally requires Linux,
Clang/LLVM, BTF, and libbpf headers.

```bash
go mod download
make build-netmon
make build-conntrack
make test
make vet
```

Privileged conntrack qualification must run as root on Linux:

```bash
sudo tests/conntrack/e2e/run-host.sh ./conntrack-linux-amd64
```

From Windows, `tests/conntrack/e2e/run-matrix.ps1` runs the same binary across
the maintained kernel matrix. See the E2E
[instructions](tests/conntrack/e2e/README.md) for bundle qualification.

## Documentation

Start with the [documentation index](docs/README.md). The authoritative project
status and prioritized backlog are in
[STATUS_AND_PLAN.md](docs/STATUS_AND_PLAN.md). Historical implementation plans
are retained for traceability but do not define current work.

Useful references:

- [Production guide (Russian)](docs/PRODUCTION_ru.md)
- [Production guide (English)](docs/PRODUCTION_en.md)
- [Conntrack module](docs/CONNTRACK.md)
- [Configuration](docs/configuration.md)
- [Testing](docs/TESTING_GUIDE.md)
- [Release process](docs/RELEASE_PROCESS.md)

## License

[MIT](LICENSE)
