# CI/CD Guide

## Workflows

| Workflow | Trigger | Purpose |
|---|---|---|
| `ci.yml` | Push/PR | Lint, race tests and Go cross-compilation checks |
| `security-scan.yml` | Push/PR/schedule | CodeQL, dependencies and gosec |
| `ebpf-build.yml` | Kernel-facing changes | Build, layout checks and blocking verifier load |
| `docker-publish.yml` | Push to `main` | Publish Linux amd64 development image |
| `release.yml` | `v*` tag | Publish Linux amd64 binaries and bundles |

Cross-compiled CI artifacts are compile checks only. The supported production
and release architecture is Linux `amd64`; ARM is not published without a real
runtime qualification host.

## Conntrack E2E

Run one host directly:

```bash
sudo tests/conntrack/e2e/run-host.sh ./conntrack-linux-amd64
```

Run the controlled kernel matrix from Windows:

```powershell
.\tests\conntrack\e2e\run-matrix.ps1 `
  -BinaryPath .\dist\conntrack-linux-amd64
```

The matrix runner prompts for credentials, pins known SSH host keys, uploads to
a unique `/tmp` directory and removes test files afterward. Passwords must
never be committed or written to configuration.

## Merge and release gates

- Required PR checks must pass before merge.
- eBPF changes require verifier and runtime evidence on supported kernels.
- A conntrack release requires the four-host E2E result and bundle lifecycle
  qualification described in `docs/RELEASE_PROCESS.md`.
- Docker Publish must succeed after merge; its supported platform is
  `linux/amd64`.

GitHub provides `GITHUB_TOKEN` for release and GHCR permissions. Do not add
long-lived credentials unless a workflow has an explicit documented need.
