package integration

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/vponomarev/network-monitor/internal/config"
	"github.com/vponomarev/network-monitor/internal/metrics"
	"go.uber.org/zap"
)

// TestMetrics_Integration tests Prometheus metrics endpoint
func TestMetrics_Integration(t *testing.T) {
	logger := zap.NewNop()
	cfg := config.PrometheusConfig{
		Enabled: true,
		Port:    19100,
		Path:    "/metrics",
	}

	server := metrics.NewServer(cfg.Port, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := server.Start(ctx)
	require.NoError(t, err)

	// Wait for server to start
	<-time.After(100 * time.Millisecond)

	// Test metrics endpoint
	resp, err := http.Get("http://localhost:19100/metrics")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Test health endpoint
	resp, err = http.Get("http://localhost:19100/health")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	cancel()
}

// TestMetrics_Record tests metric recording
func TestMetrics_Record(t *testing.T) {
	// Record various metrics
	metrics.RecordPacketLoss("eth0", true, 1.5)
	metrics.RecordPacketLoss("eth0", false, 1.0)
	metrics.RecordConnection("outgoing", "tcp")
	metrics.RecordConnection("incoming", "udp")
	metrics.RecordLatency("8.8.8.8", 50*time.Millisecond)
	metrics.RecordBandwidth("eth0", "rx", 1024.5)
	metrics.RecordBandwidth("eth0", "tx", 512.25)
	metrics.RecordDNSQuery("success", "8.8.8.8", 10*time.Millisecond)
	metrics.RecordConnectionClose()

	// Test passes if no panic
}
