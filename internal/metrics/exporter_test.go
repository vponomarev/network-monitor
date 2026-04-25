package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vponomarev/network-monitor/internal/metadata"
	"go.uber.org/zap"
)

func TestNewExporter(t *testing.T) {
	logger := zap.NewNop()
	locationMatcher := metadata.NewEmptyLocationMatcher()
	roleMatcher := metadata.NewEmptyRoleMatcher()
	reg := prometheus.NewRegistry()

	exporter := NewExporterWithRegistry("test_tcp_loss_total", locationMatcher, roleMatcher, logger, reg)

	require.NotNil(t, exporter)
	assert.Equal(t, "test_tcp_loss_total", exporter.metricName)
}

func TestExporter_RecordRetransmit(t *testing.T) {
	logger := zap.NewNop()
	locationMatcher := metadata.NewEmptyLocationMatcher()
	roleMatcher := metadata.NewEmptyRoleMatcher()
	reg := prometheus.NewRegistry()

	exporter := NewExporterWithRegistry("test_tcp_loss_total_2", locationMatcher, roleMatcher, logger, reg)

	// Record some retransmits
	exporter.RecordRetransmit("192.168.1.10", "192.168.1.20")
	exporter.RecordRetransmit("192.168.1.10", "192.168.1.20")
	exporter.RecordRetransmit("192.168.1.10", "192.168.1.30")

	assert.Equal(t, 2, exporter.GetEventCount())
}

func TestExporter_getNetwork(t *testing.T) {
	tests := []struct {
		ip       string
		expected string
	}{
		{"192.168.1.10", "192.168.1.0/24"},
		{"10.0.0.1", "10.0.0.0/24"},
		{"invalid", "0.0.0.0/24"},
	}

	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			result := getNetwork(tt.ip)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExporter_splitIP(t *testing.T) {
	tests := []struct {
		ip       string
		expected []string
	}{
		{"192.168.1.10", []string{"192", "168", "1", "10"}},
		{"10.0.0.1", []string{"10", "0", "0", "1"}},
		{"invalid", []string{"invalid"}},
	}

	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			result := splitIP(tt.ip)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExporter_SetTTL(t *testing.T) {
	logger := zap.NewNop()
	locationMatcher := metadata.NewEmptyLocationMatcher()
	roleMatcher := metadata.NewEmptyRoleMatcher()
	reg := prometheus.NewRegistry()

	exporter := NewExporterWithRegistry("test_tcp_loss_total_3", locationMatcher, roleMatcher, logger, reg)
	exporter.SetTTL(1 * time.Hour)

	// Can't directly test TTL value, but can verify no panic
	assert.NotNil(t, exporter)
}

func TestExporter_CleanupOld(t *testing.T) {
	logger := zap.NewNop()
	locationMatcher := metadata.NewEmptyLocationMatcher()
	roleMatcher := metadata.NewEmptyRoleMatcher()
	reg := prometheus.NewRegistry()

	exporter := NewExporterWithRegistry("test_tcp_loss_total_4", locationMatcher, roleMatcher, logger, reg)
	exporter.SetTTL(1 * time.Millisecond)

	// Record a retransmit
	exporter.RecordRetransmit("192.168.1.10", "192.168.1.20")
	assert.Equal(t, 1, exporter.GetEventCount())

	// Wait for TTL to expire
	time.Sleep(10 * time.Millisecond)

	// Trigger cleanup (happens automatically in Collect)
	exporter.cleanupOld()
	assert.Equal(t, 0, exporter.GetEventCount())
}

func TestExporter_Collect(t *testing.T) {
	logger := zap.NewNop()
	locationMatcher := metadata.NewEmptyLocationMatcher()
	roleMatcher := metadata.NewEmptyRoleMatcher()
	reg := prometheus.NewRegistry()

	exporter := NewExporterWithRegistry("test_tcp_loss_total_5", locationMatcher, roleMatcher, logger, reg)

	// Record some retransmits
	exporter.RecordRetransmit("192.168.1.10", "192.168.1.20")

	// Collect metrics
	ch := make(chan prometheus.Metric, 10)
	go func() {
		exporter.Collect(ch)
		close(ch)
	}()

	// Verify we get metrics
	var metrics []prometheus.Metric
	for m := range ch {
		metrics = append(metrics, m)
	}

	assert.NotEmpty(t, metrics)
}
