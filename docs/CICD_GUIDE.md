# CI/CD Guide

Complete guide to Continuous Integration and Deployment for Network Monitor.

---

## Overview

The project uses GitHub Actions for automated CI/CD pipelines.

### Workflows

| Workflow | File | Trigger | Purpose |
|----------|------|---------|---------|
| **CI** | `ci.yml` | Push/PR to main | Lint, test, build, security |
| **Release** | `release.yml` | Git tag (v*) | Build binaries, create release, publish Docker |
| **Docker Publish** | `docker-publish.yml` | Push to main | Build dev Docker images |

---

## CI Workflow

### Trigger

- Push to `main` branch
- Pull requests to `main`

### Jobs

#### 1. Lint
- Runs `golangci-lint`
- Ensures code quality

#### 2. Test
- Runs `go test -race`
- Generates coverage report
- Uploads to Codecov

#### 3. Build
- Builds for 4 platforms:
  - Linux amd64
  - Linux arm64
  - macOS amd64
  - macOS arm64
- Uploads artifacts

#### 4. Security
- Runs `gosec` (security scanner)
- Runs `govulncheck` (vulnerability check)

#### 5. Docker (PR only)
- Builds Docker image
- Tests build process

#### 6. Summary
- Aggregates results
- Reports overall status

### Badges

Add to README.md:

```markdown
[![CI](https://github.com/vponomarev/network-monitor/actions/workflows/ci.yml/badge.svg)](https://github.com/vponomarev/network-monitor/actions/workflows/ci.yml)
```

---

## Release Workflow

### Trigger

- Push of version tag (e.g., `v1.0.0`)

### Jobs

#### 1. Validate Tag
- Checks tag format
- Determines if pre-release

#### 2. Build Binaries
- Builds for all platforms
- Generates SHA256 checksums
- Uploads artifacts

#### 3. Docker Images
- Multi-architecture build (amd64, arm64)
- Pushes to ghcr.io
- Tags: version, major.minor, major, latest

#### 4. Create Release
- Generates changelog
- Creates GitHub release
- Uploads binaries and checksums

#### 5. Docker Manifest
- Creates multi-arch manifest
- Enables `docker pull` for any architecture

#### 6. Notify
- Reports success/failure
- Provides links

### Version Tags

```bash
# Stable release
git tag -a v1.0.0 -m "Release v1.0.0"

# Pre-release
git tag -a v1.0.0-beta.1 -m "Beta release 1"

# Push tag
git push origin v1.0.0
```

---

## Docker Publish Workflow

### Trigger

- Push to `main` branch

### Purpose

- Builds development Docker images
- Tags with commit SHA
- Pushes to ghcr.io

### Usage

```bash
# Pull dev image
docker pull ghcr.io/vponomarev/network-monitor:main

# Pull by commit
docker pull ghcr.io/vponomarev/network-monitor:abc1234
```

---

## Configuration

### Required Secrets

| Secret | Purpose | Required |
|--------|---------|----------|
| `GITHUB_TOKEN` | Auto-provided by GitHub | Yes |

### Optional Secrets

| Secret | Purpose | Used By |
|--------|---------|---------|
| `CODECOV_TOKEN` | Upload coverage | CI |
| `DOCKERHUB_TOKEN` | Push to Docker Hub | Release |

### Permissions

```yaml
permissions:
  contents: write    # Create releases
  packages: write    # Push Docker images
  id-token: write    # OIDC signing (future)
```

---

## Artifacts

### CI Artifacts

Retained for 7 days:

- `netmon-linux-amd64`
- `netmon-linux-arm64`
- `netmon-darwin-amd64`
- `netmon-darwin-arm64`

### Release Assets

Permanent:

- Binary files
- `checksums.txt`
- Auto-generated changelog

---

## Docker Images

### Registry

GitHub Container Registry: `ghcr.io/vponomarev/network-monitor`

### Tags

| Tag | Description |
|-----|-------------|
| `latest` | Latest stable release |
| `1` | Latest v1.x.x |
| `1.0` | Latest v1.0.x |
| `1.0.0` | Specific release |
| `main` | Latest dev build |
| `<sha>` | Specific commit |

### Pull Examples

```bash
# Latest stable
docker pull ghcr.io/vponomarev/network-monitor:latest

# Specific version
docker pull ghcr.io/vponomarev/network-monitor:v1.0.0

# Dev build
docker pull ghcr.io/vponomarev/network-monitor:main
```

---

## Local Testing

### Test CI Locally

```bash
# Install act (GitHub Actions runner)
brew install act

# Run CI locally
act -j lint
act -j test
act -j build
```

### Test Release Locally

```bash
# Create test tag
git tag -a v0.0.1-test -m "Test release"

# Push tag
git push origin v0.0.1-test

# Monitor actions
# https://github.com/vponomarev/network-monitor/actions

# Delete test tag after testing
git tag -d v0.0.1-test
git push origin :refs/tags/v0.0.1-test
```

---

## Troubleshooting

### CI Failed

**Check logs:**
```
https://github.com/vponomarev/network-monitor/actions
```

**Common issues:**

1. **Lint failed**
   ```bash
   golangci-lint run ./...
   ```

2. **Test failed**
   ```bash
   go test -v ./...
   ```

3. **Build failed**
   ```bash
   go build ./cmd/netmon
   ```

### Release Failed

**Check:**
- Tag format (must be `v*`)
- Permissions (contents: write, packages: write)
- Previous release with same tag

**Re-run:**
```bash
# Delete failed tag
git tag -d v1.0.0
git push origin :refs/tags/v1.0.0

# Fix issues
# ...

# Re-create tag
git tag -a v1.0.0 -m "Release v1.0.0"
git push origin v1.0.0
```

### Docker Push Failed

**Check:**
- GITHUB_TOKEN permissions
- Registry login
- Image name matches workflow

**Fix:**
```yaml
# In workflow file
permissions:
  packages: write
```

---

## Best Practices

### 1. Commit Messages

Use conventional commits for auto-changelog:

```bash
feat: add TCP traceroute support
fix: handle missing trace_pipe events
docs: update deployment guide
```

### 2. Pull Requests

- Keep PRs small and focused
- Add descriptive title
- Link related issues
- Request review from maintainers

### 3. Tags

- Use annotated tags: `git tag -a`
- Follow semver: `vMAJOR.MINOR.PATCH`
- Write meaningful tag messages

### 4. Releases

- Test before releasing
- Update documentation
- Add release notes
- Announce major releases

### 5. Security

- Review dependencies regularly
- Enable Dependabot
- Scan for vulnerabilities
- Sign commits (optional)

---

## Monitoring

### GitHub Actions

```
https://github.com/vponomarev/network-monitor/actions
```

### Docker Images

```
https://github.com/vponomarev/network-monitor/pkgs/container/network-monitor
```

### Releases

```
https://github.com/vponomarev/network-monitor/releases
```

---

## Advanced Configuration

### Skip CI

Add to commit message:
```
[skip ci]
```

### Manual Trigger

Add to workflow:
```yaml
on:
  workflow_dispatch:
    inputs:
      version:
        description: 'Version to release'
        required: true
```

### Custom Runners

For faster builds, use self-hosted runners:

```yaml
runs-on: [self-hosted, linux, x64]
```

---

## Future Enhancements

- [ ] SLSA provenance
- [ ] Sigstore/cosign signing
- [ ] Automated changelog to Slack/Discord
- [ ] Performance benchmarking
- [ ] Automated security scanning

---

## Support

- **CI Issues:** Check GitHub Actions logs
- **Release Issues:** Contact maintainers
- **Documentation:** See `docs/RELEASE_PROCESS.md`

---

*Automated CI/CD for reliable, consistent releases!*
