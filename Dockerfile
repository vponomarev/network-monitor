# Multi-stage Dockerfile for Network Monitor
# Supports building both netmon and conntrack applications
#
# Usage:
#   docker build -t netmon:latest --target netmon .
#   docker build -t conntrack:latest --target conntrack .
#   docker build -t network-monitor:latest .  # builds both

# =============================================================================
# BASE IMAGE
# =============================================================================
FROM golang:1.24-alpine AS base

WORKDIR /build

# Install build dependencies
RUN apk add --no-cache \
    git \
    ca-certificates \
    tzdata \
    make

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# =============================================================================
# BPF BUILDER (for conntrack)
# =============================================================================
FROM base AS bpf-builder

# Install eBPF build dependencies
RUN apk add --no-cache \
    clang \
    llvm \
    elfutils-dev \
    zlib-dev \
    linux-headers

WORKDIR /build/bpf

# Build eBPF programs
RUN make -C /build/bpf all

# =============================================================================
# NETMON BUILDER
# =============================================================================
FROM base AS netmon-builder

ARG VERSION=dev
ARG BUILD_TIME=${BUILD_TIME:-unknown}
ARG GIT_COMMIT=${GIT_COMMIT:-unknown}

# Build netmon binary
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-w -s -X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME} -X main.GitCommit=${GIT_COMMIT}" \
    -o /build/bin/netmon \
    ./cmd/netmon

# =============================================================================
# CONNTRACK BUILDER
# =============================================================================
FROM bpf-builder AS conntrack-builder

ARG VERSION=dev
ARG BUILD_TIME=${BUILD_TIME:-unknown}
ARG GIT_COMMIT=${GIT_COMMIT:-unknown}

# Copy built eBPF programs
COPY --from=bpf-builder /build/bpf/*.o /build/bpf/

# Build conntrack binary
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-w -s -X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME} -X main.GitCommit=${GIT_COMMIT}" \
    -o /build/bin/conntrack \
    ./cmd/conntrack

# =============================================================================
# NETMON RUNTIME
# =============================================================================
FROM alpine:3.19 AS netmon

# Install runtime dependencies
RUN apk --no-cache add \
    ca-certificates \
    tzdata \
    wget

# Create non-root user
RUN addgroup -g 1000 netmon && \
    adduser -D -u 1000 -G netmon netmon

# Copy binary from builder
COPY --from=netmon-builder /build/bin/netmon /usr/local/bin/netmon

# Create directories
RUN mkdir -p /etc/netmon /var/lib/netmon && \
    chown -R netmon:netmon /etc/netmon /var/lib/netmon

# Copy example configs
COPY --chown=netmon:netmon configs/*.yaml /etc/netmon/

WORKDIR /var/lib/netmon

# Switch to non-root user
USER netmon

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:9876/health || exit 1

# Expose metrics port
EXPOSE 9876

# Labels
LABEL maintainer="Network Monitor Team" \
      description="Linux network monitoring suite for TCP packet loss tracking" \
      version="1.0.0" \
      capabilities="CAP_SYS_ADMIN,CAP_NET_RAW" \
      requirements="hostNetwork,tracefs"

ENTRYPOINT ["/usr/local/bin/netmon"]
CMD ["--config", "/etc/netmon/config.yaml"]

# =============================================================================
# CONNTRACK RUNTIME
# =============================================================================
FROM alpine:3.19 AS conntrack

# Install runtime dependencies
RUN apk --no-cache add \
    ca-certificates \
    tzdata \
    libbpf \
    syslog-ng

# Create non-root user (needs to run as root for eBPF)
# Note: eBPF programs require elevated privileges

# Copy binary from builder
COPY --from=conntrack-builder /build/bin/conntrack /usr/local/bin/conntrack
COPY --from=conntrack-builder /build/bpf/*.o /usr/share/conntrack/bpf/

# Create directories
RUN mkdir -p /etc/conntrack /var/lib/conntrack

# Copy example configs
COPY configs/*.yaml /etc/conntrack/

WORKDIR /var/lib/conntrack

# Labels
LABEL maintainer="Network Monitor Team" \
      description="eBPF-based connection tracker for network connections" \
      version="1.0.0" \
      capabilities="CAP_BPF,CAP_PERFMON,CAP_NET_RAW" \
      requirements="hostNetwork"

# Note: conntrack must run as root for eBPF programs
ENTRYPOINT ["/usr/local/bin/conntrack"]
CMD ["--config", "/etc/conntrack/config.yaml"]

# =============================================================================
# COMBINED IMAGE (default target)
# =============================================================================
FROM alpine:3.19 AS combined

# Install runtime dependencies
RUN apk --no-cache add \
    ca-certificates \
    tzdata \
    libbpf \
    syslog-ng \
    wget

# Create non-root user
RUN addgroup -g 1000 netmon && \
    adduser -D -u 1000 -G netmon netmon

# Copy binaries from builders
COPY --from=netmon-builder /build/bin/netmon /usr/local/bin/netmon
COPY --from=conntrack-builder /build/bin/conntrack /usr/local/bin/conntrack
COPY --from=conntrack-builder /build/bpf/*.o /usr/share/conntrack/bpf/

# Create directories
RUN mkdir -p /etc/netmon /etc/conntrack /var/lib/netmon /var/lib/conntrack && \
    chown -R netmon:netmon /etc/netmon /var/lib/netmon

# Copy example configs
COPY --chown=netmon:netmon configs/*.yaml /etc/netmon/
COPY configs/*.yaml /etc/conntrack/

WORKDIR /var/lib/netmon

# Labels
LABEL maintainer="Network Monitor Team" \
      description="Network Monitor - Combined image with netmon and conntrack" \
      version="1.0.0" \
      capabilities="CAP_SYS_ADMIN,CAP_BPF,CAP_PERFMON,CAP_NET_RAW" \
      requirements="hostNetwork,tracefs"

# Health check for netmon
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:9876/health || exit 1

# Expose metrics port
EXPOSE 9876

# Default command runs netmon (conntrack can be started separately)
# Note: Must run as root for full functionality
ENTRYPOINT ["/usr/local/bin/netmon"]
CMD ["--config", "/etc/netmon/config.yaml"]
