# Repository Guidelines

## Project Structure & Module Organization

`cmd/netmon` is the production TCP-loss entry point. `cmd/conntrack` is the production-qualified standalone TCP lifecycle tracker released in v2.2.0. Core Go packages live under `internal/` (collection, metrics, configuration, discovery, health, and topology). Shared event and embedded-resource packages are in `pkg/`. eBPF sources are under `bpf/`; generated `.o` files used by single-binary builds belong in `pkg/embedded/bpf/`.

Configuration examples are in `configs/`, deployment assets in `packaging/`, dashboards in `dashboards/`, and operational/design documentation in `docs/`. Unit tests are colocated as `*_test.go`; privileged and cross-component tests live in `tests/` and `tests/integration/`.

## Build, Test, and Development Commands

- `go mod download` installs Go dependencies (Go 1.21+).
- `make build-netmon` builds the production binary at `bin/netmon`.
- `make ebpf-build` compiles eBPF objects; it requires Linux, Clang, kernel headers/BTF, and libbpf tooling.
- `make test` runs all Go tests with verbose output and the race detector.
- `make test-coverage` additionally writes `coverage.out` and `coverage.html`.
- `make vet` runs `go vet ./...`; `make lint` runs `golangci-lint` when installed.
- `make fmt` applies `gofmt -s`; run `make check` before opening a PR.

Privileged integration tests (`make test-integration`) must run on Linux as root. Do not assume hosted runners can load eBPF on kernels newer than the supported matrix.

## Coding Style & Naming Conventions

Use standard Go formatting and idioms: tabs as emitted by `gofmt`, short lower-camel local names, and PascalCase exported identifiers. Keep packages lowercase and focused. Wrap errors with context, for example `fmt.Errorf("loading config: %w", err)`. C/eBPF changes must remain verifier-friendly and avoid unbounded work.

## Testing Guidelines

Name tests `TestXxx` and prefer table-driven cases with descriptive subtest names. Add tests beside changed Go code. Changes to eBPF collection require both Go tests and a Linux load/runtime smoke test; document tested kernel versions in the PR.

For conntrack release qualification, run `tests/conntrack/e2e/run-host.sh` on Linux or use `tests/conntrack/e2e/run-matrix.ps1 -BinaryPath <conntrack-linux-amd64>` from Windows. Release artifacts are Linux amd64 only; cross-compilation does not establish runtime support for another architecture.

Use the following kernel matrix for remote eBPF checks. Connect over SSH as `root`; request the current password from the user before testing and never store it in files, commands committed to Git, or documentation.

| Host | OS | Kernel |
| --- | --- | --- |
| `192.168.5.99` | Proxmox VE 8 / Debian 12 | `6.8.12-20-pve` |
| `192.168.5.193` | Debian 12 | `6.1.0-45` |
| `192.168.5.214` | Debian 13 | `6.12.85` |
| `192.168.5.217` | Ubuntu 22.04 LTS | `5.15.0-185` |

## Commit & Pull Request Guidelines

Use Conventional Commits seen in history: `feat:`, `fix:`, `ci:`, `docs:`, `test:`, or scoped forms such as `fix(collector): ...`. Keep commits narrow. PRs should explain behavior and risk, list test commands/results, link relevant issues, and update configuration or docs when interfaces change. All required GitHub checks must pass before merge; screenshots are needed only for dashboard/UI changes.

## Security & Configuration

Never commit credentials, host passwords, generated coverage files, or local configs. Base examples on `configs/*.example.yaml`. Treat kernel-facing changes as high risk and preserve observability for dropped events and partial failures.
