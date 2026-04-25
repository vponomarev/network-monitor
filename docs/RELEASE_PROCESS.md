# Release Process

This document describes the automated release process using GitHub Actions.

---

## Overview

The project uses GitHub Actions for automated CI/CD:

| Workflow | Trigger | Purpose |
|----------|---------|---------|
| **CI** | Push/PR to main | Lint, test, build, security scan |
| **Release** | Git tag (v*) | Build binaries, create GitHub release, publish Docker |
| **Docker Publish** | Push to main | Build and push dev Docker images |

---

## Creating a Release

### 1. Update Version

Update version in your code if needed (e.g., `cmd/netmon/main.go`):

```go
var Version = "v1.0.0"
```

### 2. Create and Push Tag

```bash
# Ensure you're on main
git checkout main
git pull origin main

# Create annotated tag
git tag -a v1.0.0 -m "Release v1.0.0"

# Push tag to GitHub
git push origin v1.0.0
```

### 3. Automated Process

Once the tag is pushed, GitHub Actions will:

1. **Validate tag** - Check version format
2. **Build binaries** - For all platforms (linux/amd64, linux/arm64, darwin/amd64, darwin/arm64)
3. **Build Docker images** - Multi-architecture (amd64, arm64)
4. **Generate checksums** - SHA256 for all binaries
5. **Create GitHub Release** - With changelog and assets
6. **Push Docker images** - To ghcr.io

### 4. Monitor Progress

Go to: https://github.com/vponomarev/network-monitor/actions

Wait for all jobs to complete (~5-10 minutes).

### 5. Verify Release

Check:
- GitHub Releases: https://github.com/vponomarev/network-monitor/releases
- Docker images: https://github.com/vponomarev/network-monitor/pkgs/container/network-monitor

---

## Version Numbering

This project uses [Semantic Versioning](https://semver.org/):

```
MAJOR.MINOR.PATCH
  │     │     │
  │     │     └─ Bug fixes (backward compatible)
  │     └─────── New features (backward compatible)
  └───────────── Breaking changes
```

### Examples

| Version | Type | Description |
|---------|------|-------------|
| `v1.0.0` | Major | Initial release |
| `v1.0.1` | Patch | Bug fixes |
| `v1.1.0` | Minor | New features |
| `v2.0.0` | Major | Breaking changes |

### Pre-releases

For alpha/beta/rc versions:

```
v1.0.0-alpha.1
v1.0.0-beta.1
v1.0.0-rc.1
```

Pre-releases are marked as "pre-release" on GitHub and don't get the `latest` Docker tag.

---

## Release Assets

Each release includes:

### Binaries

| File | Platform | Architecture |
|------|----------|--------------|
| `netmon-linux-amd64` | Linux | x86_64 |
| `netmon-linux-arm64` | Linux | ARM64 |
| `netmon-darwin-amd64` | macOS | x86_64 |
| `netmon-darwin-arm64` | macOS | ARM64 (M1/M2) |

### Checksums

- `checksums.txt` - SHA256 checksums for all binaries

### Docker Images

- `ghcr.io/vponomarev/network-monitor:<version>` - Specific version
- `ghcr.io/vponomarev/network-monitor:<major>.<minor>` - Minor version tag
- `ghcr.io/vponomarev/network-monitor:<major>` - Major version tag
- `ghcr.io/vponomarev/network-monitor:latest` - Latest stable (not for pre-releases)

---

## Manual Release (Fallback)

If automated release fails:

### 1. Build Binaries

```bash
# Linux amd64
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o netmon-linux-amd64 ./cmd/netmon

# Linux arm64
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-s -w" -o netmon-linux-arm64 ./cmd/netmon

# macOS amd64
GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o netmon-darwin-amd64 ./cmd/netmon

# macOS arm64
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-s -w" -o netmon-darwin-arm64 ./cmd/netmon
```

### 2. Generate Checksums

```bash
sha256sum netmon-* > checksums.txt
```

### 3. Create GitHub Release

1. Go to https://github.com/vponomarev/network-monitor/releases/new
2. Tag version: `v1.0.0`
3. Release title: `Release v1.0.0`
4. Add changelog
5. Upload binaries and checksums
6. Mark as pre-release if needed
7. Publish

### 4. Build and Push Docker

```bash
# Login
docker login ghcr.io -vponomarev

# Build
docker build -t ghcr.io/vponomarev/network-monitor:v1.0.0 .

# Push
docker push ghcr.io/vponomarev/network-monitor:v1.0.0
```

---

## Changelog

The changelog is automatically generated from:

1. Git commits between tags
2. Pull request titles and descriptions
3. Labels on PRs

### Label Mapping

| Label | Section |
|-------|---------|
| `feature`, `enhancement` | 🚀 Features |
| `bug`, `fix` | 🐛 Bug Fixes |
| `documentation` | 📝 Documentation |
| `chore`, `refactor` | 🔧 Maintenance |
| `performance` | ⚡ Performance |
| `security` | 🔒 Security |
| `tests` | 🧪 Tests |
| `dependencies` | 📦 Dependencies |

### Skip Changelog

Add `skip-changelog` label to exclude from changelog.

---

## Troubleshooting

### Release Workflow Failed

**Check logs:**
```bash
# Go to Actions tab
https://github.com/vponomarev/network-monitor/actions
```

**Common issues:**

1. **Build failed** - Check Go version, dependencies
2. **Docker push failed** - Check GITHUB_TOKEN permissions
3. **Release creation failed** - Check tag format (must be v*)

### Docker Image Not Published

**Check:**
- Repository name matches workflow condition
- GITHUB_TOKEN has `packages: write` permission
- Docker build completed successfully

### Checksums Don't Match

**Regenerate:**
```bash
cd artifacts
sha256sum netmon-* > checksums.txt
```

---

## Security

### Token Permissions

The workflows require these permissions:

```yaml
permissions:
  contents: write    # Create releases, upload assets
  packages: write    # Push Docker images
  id-token: write    # OIDC for provenance (future)
```

### Binary Signing (Future)

Consider adding:
- Sigstore/cosign for Docker image signing
- GPG signing for binaries
- SLSA provenance

---

## Best Practices

1. **Always use annotated tags:**
   ```bash
   git tag -a v1.0.0 -m "Release v1.0.0"
   ```

2. **Test before release:**
   ```bash
   # Run all tests
   make test
   
   # Build locally
   make build
   ```

3. **Update documentation:**
   - README.md
   - CHANGELOG.md (if not auto-generated)
   - Deployment guides

4. **Communicate:**
   - Announce major releases
   - Update users about breaking changes
   - Provide migration guide if needed

5. **Verify release:**
   - Download and test binaries
   - Pull and run Docker image
   - Check all assets are present

---

## Example Release Commands

```bash
# Prepare release
git checkout main
git pull origin main
make test
make build

# Create tag
git tag -a v1.0.0 -m "Release v1.0.0 - Initial public release"

# Push tag
git push origin v1.0.0

# Monitor
open https://github.com/vponomarev/network-monitor/actions

# Verify release
open https://github.com/vponomarev/network-monitor/releases

# Pull Docker image
docker pull ghcr.io/vponomarev/network-monitor:v1.0.0
```

---

## Support

- **CI Issues:** Check GitHub Actions logs
- **Release Issues:** Contact maintainers
- **Security Issues:** Use security advisory

---

*Automated releases for consistent, reliable deployments!*
