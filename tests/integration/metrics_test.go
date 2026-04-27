package integration

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vponomarev/network-monitor/internal/metadata"
	"github.com/vponomarev/network-monitor/internal/metrics"
	"go.uber.org/zap"
)

// TestMetrics_Server_Integration tests Prometheus metrics server
func TestMetrics_Server_Integration(t *testing.T) {
	logger := zap.NewNop()

	// Create exporter
	locationMatcher := metadata.NewEmptyLocationMatcher()
	roleMatcher := metadata.NewEmptyRoleMatcher()
	exporter := metrics.NewExporter("test_metric", locationMatcher, roleMatcher, logger)

	// Create registry
	reg := prometheus.NewRegistry()
	reg.MustRegister(exporter)

	// Start HTTP server
	port := 19101
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}

	// Start server in background
	go func() {
		_ = server.ListenAndServe()
	}()

	// Wait for server to start
	<-time.After(100 * time.Millisecond)

	// Test metrics endpoint
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/metrics", port))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Test health endpoint
	resp, err = http.Get(fmt.Sprintf("http://localhost:%d/health", port))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Shutdown server
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
}

// TestMetrics_Record tests metric recording
func TestMetrics_Record(t *testing.T) {
	logger := zap.NewNop()
	locationMatcher := metadata.NewEmptyLocationMatcher()
	roleMatcher := metadata.NewEmptyRoleMatcher()
	
	// Create a new registry for this test to avoid duplicate registration
	reg := prometheus.NewRegistry()
	exporter := metrics.NewExporterWithRegistry("test_metric_record", locationMatcher, roleMatcher, logger, reg)

	// Record some metrics
	exporter.RecordRetransmit("192.168.1.10", "192.168.1.20")
	exporter.RecordRetransmit("192.168.1.10", "192.168.1.20")
	exporter.RecordRetransmit("192.168.1.10", "192.168.1.30")

	// Verify metrics were recorded
	assert.Equal(t, 2, exporter.GetEventCount())
}
