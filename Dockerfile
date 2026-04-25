# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /build

# Install dependencies
RUN apk add --no-cache git ca-certificates

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build binary
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-w -s -X main.Version=${VERSION:-dev} -X main.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ) -X main.GitCommit=$(git rev-parse --short HEAD 2>/dev/null || echo unknown)" \
    -o /build/netmon \
    ./cmd/netmon

# Runtime stage
FROM alpine:3.19

# Install ca-certificates for HTTPS
RUN apk --no-cache add ca-certificates tzdata

# Create non-root user
RUN addgroup -g 1000 netmon && \
    adduser -D -u 1000 -G netmon netmon

# Copy binary from builder
COPY --from=builder /build/netmon /usr/local/bin/netmon

# Create config directory
RUN mkdir -p /etc/netmon && \
    chown -R netmon:netmon /etc/netmon

# Create data directory for metrics
RUN mkdir -p /var/lib/netmon && \
    chown -R netmon:netmon /var/lib/netmon

# Switch to non-root user
USER netmon

# Default command
ENTRYPOINT ["/usr/local/bin/netmon"]
CMD ["--config", "/etc/netmon/config.yaml"]

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:9876/health || exit 1

# Expose metrics port
EXPOSE 9876

# Labels
LABEL maintainer="Network Monitor Team"
LABEL description="Linux network monitoring suite for TCP packet loss tracking"
LABEL version="1.0.0"
LABEL capabilities="CAP_SYS_ADMIN,CAP_NET_RAW"
LABEL requirements="hostNetwork,tracefs"
