# GitHub Actions CI/CD

Automated CI/CD pipelines for Network Monitor.

## Workflows

### 1. CI (`ci.yml`)

**Triggers:** Push/PR to main branch

**Jobs:**
- ✅ **Lint** - golangci-lint
- ✅ **Test** - go test with coverage
- ✅ **Build** - Cross-compilation checks; release artifacts are Linux amd64 only
- ✅ **Security** - gosec + govulncheck
- ✅ **Docker** - Build test (PR only)

### 2. Release (`release.yml`)

**Triggers:** Git tag push (v*)

**Jobs:**
- 🏗️ Build qualified Linux `amd64` binaries and bundles
- 📦 Create GitHub release with changelog
- 🔐 Generate and upload checksums

### 3. Docker Publish (`docker-publish.yml`)

**Triggers:** Push to main

**Jobs:**
- 🐳 Build and push Linux `amd64` dev Docker images
- 🏷️ Tag with SHA and branch name

## Quick Start

### Create Release

```bash
# Tag release
git tag -a v1.0.0 -m "Release v1.0.0"

# Push tag
git push origin v1.0.0

# Monitor
# https://github.com/vponomarev/network-monitor/actions
```

### Verify Release

```bash
# Check GitHub Releases
# https://github.com/vponomarev/network-monitor/releases

# Pull Docker image
docker pull ghcr.io/vponomarev/network-monitor:latest
```

## Badges

```markdown
[![CI](https://github.com/vponomarev/network-monitor/actions/workflows/ci.yml/badge.svg)](https://github.com/vponomarev/network-monitor/actions/workflows/ci.yml)
[![Release](https://github.com/vponomarev/network-monitor/actions/workflows/release.yml/badge.svg)](https://github.com/vponomarev/network-monitor/actions/workflows/release.yml)
```

## Files

| File | Purpose |
|------|---------|
| `.github/workflows/ci.yml` | CI pipeline |
| `.github/workflows/release.yml` | Release automation |
| `.github/workflows/docker-publish.yml` | Docker image builds |
| `.github/release-config.yml` | Changelog configuration |

## Documentation

- [Release Process](../docs/RELEASE_PROCESS.md)
- [CI/CD Guide](../docs/CICD_GUIDE.md)
- [Contributing](../CONTRIBUTING.md)
