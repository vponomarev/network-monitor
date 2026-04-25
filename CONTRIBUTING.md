# Contributing to Network Monitor

Thank you for considering contributing to Network Monitor! This document provides guidelines and instructions for contributing.

---

## Table of Contents

- [Code of Conduct](#code-of-conduct)
- [Getting Started](#getting-started)
- [Development Workflow](#development-workflow)
- [Pull Requests](#pull-requests)
- [Release Process](#release-process)
- [Style Guide](#style-guide)

---

## Code of Conduct

- Be respectful and inclusive
- Focus on constructive feedback
- Welcome newcomers and help them learn

---

## Getting Started

### 1. Fork the Repository

```bash
# Click "Fork" on GitHub
# Clone your fork
git clone https://github.com/YOUR_USERNAME/network-monitor.git
cd network-monitor

# Add upstream remote
git remote add upstream https://github.com/vponomarev/network-monitor.git
```

### 2. Set Up Development Environment

```bash
# Install Go 1.21+
go version

# Install dependencies
go mod download

# Install linters
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
```

### 3. Create Branch

```bash
# Sync with upstream
git checkout main
git pull upstream main

# Create feature branch
git checkout -b feature/your-feature-name
```

---

## Development Workflow

### Make Changes

```bash
# Edit code
vim internal/collector/trace_pipe.go

# Format code
go fmt ./...

# Run linter
golangci-lint run ./...

# Run tests
go test -v ./...

# Build
make build
```

### Commit Changes

```bash
# Stage changes
git add .

# Commit with conventional commit message
git commit -m "feat: add TCP traceroute support"
```

### Commit Message Format

We use [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>(<scope>): <description>

[optional body]

[optional footer]
```

**Types:**
- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation
- `style`: Formatting
- `refactor`: Code refactoring
- `test`: Tests
- `chore`: Maintenance

**Examples:**
```
feat(discovery): add TCP traceroute support
fix(collector): handle missing trace_pipe events
docs: update deployment guide
refactor(metrics): simplify exporter logic
```

### Push and Create PR

```bash
# Push to your fork
git push origin feature/your-feature-name

# Create PR on GitHub
# https://github.com/vponomarev/network-monitor/compare
```

---

## Pull Requests

### PR Checklist

Before submitting your PR:

- [ ] Code follows style guide
- [ ] Tests added/updated
- [ ] Documentation updated
- [ ] Commit messages follow convention
- [ ] No linting errors
- [ ] All tests pass

### PR Template

```markdown
## Description

Brief description of changes.

## Type of Change

- [ ] Bug fix
- [ ] New feature
- [ ] Breaking change
- [ ] Documentation update

## Testing

Describe how you tested the changes.

## Checklist

- [ ] Code follows project guidelines
- [ ] Self-review completed
- [ ] Tests pass locally
- [ ] Documentation updated
```

### Review Process

1. **CI Checks** - Automated tests must pass
2. **Code Review** - Maintainer reviews code
3. **Changes** - Address review feedback
4. **Merge** - Maintainer merges PR

### Labels

| Label | Meaning |
|-------|---------|
| `feature` | New feature |
| `bug` | Bug fix |
| `documentation` | Docs update |
| `tests` | Test changes |
| `dependencies` | Dependency update |
| `help wanted` | Needs help |
| `good first issue` | Good for beginners |

---

## Release Process

### For Maintainers

1. **Prepare Release**
   ```bash
   git checkout main
   git pull upstream main
   make test
   ```

2. **Create Tag**
   ```bash
   git tag -a v1.0.0 -m "Release v1.0.0"
   git push origin v1.0.0
   ```

3. **Monitor CI**
   - Watch GitHub Actions
   - Verify all jobs pass
   - Check release assets

4. **Announce**
   - Update changelog
   - Notify users
   - Update documentation

---

## Style Guide

### Go Code

Follow [Effective Go](https://golang.org/doc/effective_go) and [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments).

**Naming:**
```go
// Use camelCase for variables
var tcpLossCounter int

// Use PascalCase for exported names
type TCPConnection struct{}

// Use underscores for constants
const MaxRetries = 3
```

**Comments:**
```go
// Good: Explains why
// Use exponential backoff for retries
for i := 0; i < maxRetries; i++ {
    // ...
}

// Bad: Explains what
// Increment counter
counter++
```

**Error Handling:**
```go
// Good: Descriptive error
if err != nil {
    return fmt.Errorf("loading config: %w", err)
}

// Bad: Generic error
if err != nil {
    return err
}
```

### Documentation

- Use clear, concise language
- Include examples
- Keep up to date
- Use markdown formatting

### Tests

```go
func TestExporter_Record(t *testing.T) {
    // Arrange
    exporter := NewExporter()

    // Act
    exporter.Record(event)

    // Assert
    assert.Equal(t, expected, actual)
}
```

---

## Architecture

### Project Structure

```
network-monitor/
├── cmd/
│   └── netmon/          # Main application
├── internal/
│   ├── collector/       # trace_pipe collector
│   ├── discovery/       # Path discovery
│   ├── metrics/         # Prometheus exporter
│   ├── config/          # Configuration
│   └── metadata/        # Location/role matchers
├── configs/             # Example configs
├── docs/                # Documentation
├── scripts/             # Helper scripts
└── packaging/           # Deployment files
```

### Key Components

| Component | Purpose |
|-----------|---------|
| **Collector** | Reads trace_pipe events |
| **Discovery** | Traceroute for path discovery |
| **Metrics** | Prometheus exporter |
| **Config** | YAML configuration |
| **Metadata** | Location/role enrichment |

---

## Testing

### Run Tests

```bash
# All tests
go test -v ./...

# With coverage
go test -v -coverprofile=coverage.out ./...

# Specific package
go test -v ./internal/collector/...

# Race detection
go test -race ./...
```

### Write Tests

```go
func TestCollector_ParseEvent(t *testing.T) {
    tests := []struct {
        name     string
        input    string
        expected Event
        wantErr  bool
    }{
        {
            name:  "valid event",
            input: "tcp_retransmit_skb: ...",
            expected: Event{...},
            wantErr: false,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Test implementation
        })
    }
}
```

---

## CI/CD

### Workflows

| Workflow | Trigger | Purpose |
|----------|---------|---------|
| **CI** | Push/PR | Lint, test, build |
| **Release** | Tag | Build, release, Docker |
| **Docker Publish** | Push to main | Dev images |

### Requirements

All PRs must pass:
- ✅ Linting (golangci-lint)
- ✅ Tests (go test)
- ✅ Build (all platforms)
- ✅ Security scan (gosec, govulncheck)

---

## Getting Help

- **Documentation:** `docs/` directory
- **Issues:** GitHub Issues
- **Discussions:** GitHub Discussions
- **Email:** Contact maintainers

---

## Recognition

Contributors are recognized in:
- README.md contributors section
- Release notes
- GitHub contributors page

---

## License

By contributing, you agree that your contributions will be licensed under the project's license.

---

*Thank you for contributing to Network Monitor!*
