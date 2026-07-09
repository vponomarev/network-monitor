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
	locationMatcher := metadata.NewEmptyLocationMatcher(logger)
	roleMatcher := metadata.NewEmptyRoleMatcher(logger)
	reg := prometheus.NewRegistry()

	exporter := NewExporterWithRegistry("test_tcp_loss_total", locationMatcher, roleMatcher, logger, reg)

	require.NotNil(t, exporter)
	assert.Equal(t, "test_tcp_loss_total", exporter.metricName)
}

func TestExporter_RecordRetransmit(t *testing.T) {
	logger := zap.NewNop()
	locationMatcher := metadata.NewEmptyLocationMatcher(logger)
	roleMatcher := metadata.NewEmptyRoleMatcher(logger)
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
	locationMatcher := metadata.NewEmptyLocationMatcher(logger)
	roleMatcher := metadata.NewEmptyRoleMatcher(logger)
	reg := prometheus.NewRegistry()

	exporter := NewExporterWithRegistry("test_tcp_loss_total_3", locationMatcher, roleMatcher, logger, reg)
	exporter.SetTTL(1 * time.Hour)

	// Can't directly test TTL value, but can verify no panic
	assert.NotNil(t, exporter)
}

func TestExporter_CleanupOld(t *testing.T) {
	logger := zap.NewNop()
	locationMatcher := metadata.NewEmptyLocationMatcher(logger)
	roleMatcher := metadata.NewEmptyRoleMatcher(logger)
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
	locationMatcher := metadata.NewEmptyLocationMatcher(logger)
	roleMatcher := metadata.NewEmptyRoleMatcher(logger)
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

func TestExporter_WithVrfLabels(t *testing.T) {
	logger := zap.NewNop()
	locationMatcher := metadata.NewEmptyLocationMatcher(logger)
	roleMatcher := metadata.NewEmptyRoleMatcher(logger)
	reg := prometheus.NewRegistry()

	exporter := NewExporterWithRegistry("test_tcp_loss_total_vrf", locationMatcher, roleMatcher, logger, reg)

	// Record retransmit
	exporter.RecordRetransmit("192.168.1.10", "192.168.1.20")

	// Collect metrics
	ch := make(chan prometheus.Metric, 20)
	go func() {
		exporter.Collect(ch)
		close(ch)
	}()

	// Verify we get metrics with VRF labels
	var metrics []prometheus.Metric
	for m := range ch {
		metrics = append(metrics, m)
	}

	assert.NotEmpty(t, metrics)
	// Metrics should have 10 labels (including src_vrf, dst_vrf)
	desc := metrics[0].Desc()
	assert.Contains(t, desc.String(), "src_vrf")
	assert.Contains(t, desc.String(), "dst_vrf")
}

func TestRecordRetransmit_CountsOnePerEvent(t *testing.T) {
	logger := zap.NewNop()
	locationMatcher := metadata.NewEmptyLocationMatcher(logger)
	roleMatcher := metadata.NewEmptyRoleMatcher(logger)
	reg := prometheus.NewRegistry()

	exporter := NewExporterWithRegistry("test_tcp_loss_total_one_per_event", locationMatcher, roleMatcher, logger, reg)

	// Record 5 retransmits for the same pair
	for i := 0; i < 5; i++ {
		exporter.RecordRetransmit("10.0.0.1", "10.0.0.2")
	}

	// Collect metrics and verify count is exactly 5, not 15 (triangular number)
	metrics, err := reg.Gather()
	require.NoError(t, err)

	// Find our metric
	var found bool
	for _, m := range metrics {
		if m.GetName() == "test_tcp_loss_total_one_per_event" {
			found = true
			require.Len(t, m.GetMetric(), 1)
			assert.Equal(t, float64(5), m.GetMetric()[0].GetCounter().GetValue())
		}
	}
	require.True(t, found, "metric not found")
}

func TestCardinality_RoleLevel_NoIPLabels(t *testing.T) {
	logger := zap.NewNop()
	reg := prometheus.NewRegistry()
	exporter := NewExporterWithConfig("test_loss_role", metadata.NewEmptyLocationMatcher(logger),
		metadata.NewEmptyRoleMatcher(logger), logger, reg,
		CardinalityConfig{Level: LevelRole, MaxSeries: 0})

	exporter.RecordRetransmit("10.0.0.1", "10.0.0.2")

	metrics, err := reg.Gather()
	require.NoError(t, err)

	var found bool
	for _, m := range metrics {
		if m.GetName() != "test_loss_role" {
			continue
		}
		found = true
		require.Len(t, m.GetMetric(), 1)
		for _, lp := range m.GetMetric()[0].GetLabel() {
			assert.NotEqual(t, "src_ip", lp.GetName(), "role level must not expose src_ip")
			assert.NotEqual(t, "dst_ip", lp.GetName(), "role level must not expose dst_ip")
			assert.NotEqual(t, "src_network", lp.GetName(), "role level must not expose src_network")
		}
	}
	require.True(t, found, "metric not found")
}

func TestCardinality_NetworkLevel_AggregatesBySubnet(t *testing.T) {
	logger := zap.NewNop()
	reg := prometheus.NewRegistry()
	exporter := NewExporterWithConfig("test_loss_net", metadata.NewEmptyLocationMatcher(logger),
		metadata.NewEmptyRoleMatcher(logger), logger, reg,
		CardinalityConfig{Level: LevelNetwork, MaxSeries: 0})

	// Two distinct IP pairs within the same /24 -> one aggregated series.
	exporter.RecordRetransmit("10.0.0.1", "10.0.0.2")
	exporter.RecordRetransmit("10.0.0.3", "10.0.0.4")

	assert.Equal(t, 1, exporter.GetEventCount(), "same /24 pairs must aggregate to one series")
}

func TestCardinality_MaxSeriesCap(t *testing.T) {
	logger := zap.NewNop()
	reg := prometheus.NewRegistry()
	exporter := NewExporterWithConfig("test_loss_cap", metadata.NewEmptyLocationMatcher(logger),
		metadata.NewEmptyRoleMatcher(logger), logger, reg,
		CardinalityConfig{Level: LevelIP, MaxSeries: 2})

	// 3 distinct pairs, cap is 2 -> third is dropped.
	exporter.RecordRetransmit("10.0.0.1", "10.0.0.2")
	exporter.RecordRetransmit("10.0.0.3", "10.0.0.4")
	exporter.RecordRetransmit("10.0.0.5", "10.0.0.6") // dropped
	exporter.RecordRetransmit("10.0.0.5", "10.0.0.6") // dropped again

	assert.Equal(t, 2, exporter.GetEventCount(), "active series must not exceed max_series")

	metrics, err := reg.Gather()
	require.NoError(t, err)
	var dropped float64
	for _, m := range metrics {
		if m.GetName() == "netmon_loss_series_dropped_total" {
			dropped = m.GetMetric()[0].GetCounter().GetValue()
		}
	}
	assert.Equal(t, float64(2), dropped, "both attempts on the 3rd pair should count as dropped")
}

func TestCleanupOld_RemovesFromCounterVec(t *testing.T) {
	logger := zap.NewNop()
	locationMatcher := metadata.NewEmptyLocationMatcher(logger)
	roleMatcher := metadata.NewEmptyRoleMatcher(logger)
	reg := prometheus.NewRegistry()

	exporter := NewExporterWithRegistry("test_tcp_loss_total_cleanup", locationMatcher, roleMatcher, logger, reg)
	exporter.SetTTL(1 * time.Millisecond)

	// Record a retransmit
	exporter.RecordRetransmit("10.0.0.1", "10.0.0.2")

	// Verify we have 1 series before cleanup
	metrics, err := reg.Gather()
	require.NoError(t, err)
	var initialSeries int
	for _, m := range metrics {
		if m.GetName() == "test_tcp_loss_total_cleanup" {
			initialSeries = len(m.GetMetric())
		}
	}
	assert.Equal(t, 1, initialSeries)

	// Wait for TTL to expire
	time.Sleep(10 * time.Millisecond)

	// Trigger cleanup (Collect calls cleanupOld)
	ch := make(chan prometheus.Metric, 10)
	go func() {
		exporter.Collect(ch)
		close(ch)
	}()
	// Drain the channel
	for range ch {
	}

	// Verify series was removed from CounterVec
	metrics, err = reg.Gather()
	require.NoError(t, err)
	var finalSeries int
	for _, m := range metrics {
		if m.GetName() == "test_tcp_loss_total_cleanup" {
			finalSeries = len(m.GetMetric())
		}
	}
	assert.Equal(t, 0, finalSeries)
}
