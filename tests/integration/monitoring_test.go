// Package integration contains integration tests for Network Monitor
// These tests require root privileges and access to kernel features
// Run with: sudo go test -v ./tests/integration/...
package integration

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/vponomarev/network-monitor/internal/bandwidth"
	"github.com/vponomarev/network-monitor/internal/config"
	"github.com/vponomarev/network-monitor/internal/dns"
	"github.com/vponomarev/network-monitor/internal/latency"
	"go.uber.org/zap"
)

// skipIfNotRoot skips the test if not running as root
func skipIfNotRoot(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("Integration tests require root privileges")
	}
}

// TestBandwidth_Integration tests bandwidth monitoring with real interfaces
func TestBandwidth_Integration(t *testing.T) {
	skipIfNotRoot(t)

	logger := zap.NewNop()
	cfg := config.BandwidthConfig{
		Interfaces: []string{"lo"},
		Interval:   "100ms",
	}

	monitor := bandwidth.NewMonitor(cfg, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	// Start monitoring
	go func() {
		_ = monitor.Run(ctx)
	}()

	// Generate some traffic on loopback
	conn, err := net.Dial("tcp", "127.0.0.1:80")
	if err == nil {
		conn.Close()
	}

	// Wait for collection
	<-time.After(200 * time.Millisecond)

	// Check we got some stats
	stats := monitor.GetAllStats()
	assert.NotEmpty(t, stats)
}

// TestLatency_Integration tests latency monitoring with real targets
func TestLatency_Integration(t *testing.T) {
	skipIfNotRoot(t)
	
	logger := zap.NewNop()
	cfg := config.LatencyConfig{
		Targets:  []string{"127.0.0.1"},
		Interval: "100ms",
		Timeout:  "500ms",
	}

	monitor := latency.NewMonitor(cfg, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	// Start monitoring
	go func() {
		_ = monitor.Run(ctx)
	}()

	// Wait for measurement
	<-time.After(200 * time.Millisecond)

	// Check we got results
	results := monitor.GetAllResults()
	assert.NotEmpty(t, results)
}

// TestDNS_Integration tests DNS monitoring with real queries
func TestDNS_Integration(t *testing.T) {
	logger := zap.NewNop()
	cfg := config.DNSConfig{
		Interval: "100ms",
	}

	monitor := dns.NewMonitor(cfg, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	// Start monitoring
	go func() {
		_ = monitor.Run(ctx)
	}()

	// Wait for query
	<-time.After(200 * time.Millisecond)

	// Check we got results
	results := monitor.GetAllResults()
	assert.NotEmpty(t, results)

	// localhost should resolve successfully
	if result, ok := results["localhost"]; ok {
		assert.True(t, result.Success)
	}
}

// TestMonitor_Integration tests full monitoring stack
func TestMonitor_Integration(t *testing.T) {
	skipIfNotRoot(t)

	logger := zap.NewNop()

	// Test bandwidth
	bwCfg := config.BandwidthConfig{
		Interfaces: []string{"lo"},
		Interval:   "50ms",
	}
	bwMonitor := bandwidth.NewMonitor(bwCfg, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	go func() {
		_ = bwMonitor.Run(ctx)
	}()

	// Generate traffic
	for i := 0; i < 10; i++ {
		conn, err := net.Dial("tcp", "127.0.0.1:80")
		if err == nil {
			conn.Close()
		}
	}

	<-time.After(150 * time.Millisecond)

	stats := bwMonitor.GetStats("lo")
	assert.NotNil(t, stats)
}
