package collector

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestCollectorMetrics_Registration(t *testing.T) {
	reg := prometheus.NewRegistry()
	logger := zap.NewNop()

	metrics := NewCollectorMetrics(reg, logger)

	require.NotNil(t, metrics)

	// Verify all metrics are registered by gathering
	metricFamilies, err := reg.Gather()
	require.NoError(t, err)

	// Should have 5 metrics
	assert.Len(t, metricFamilies, 5)

	// Check metric names
	metricNames := make(map[string]bool)
	for _, mf := range metricFamilies {
		metricNames[mf.GetName()] = true
	}

	assert.True(t, metricNames["netmon_loss_collector_up"])
	assert.True(t, metricNames["netmon_loss_events_read_total"])
	assert.True(t, metricNames["netmon_loss_events_parsed_total"])
	assert.True(t, metricNames["netmon_loss_parse_errors_total"])
	assert.True(t, metricNames["netmon_loss_source_info"])
}

func TestCollectorMetrics_SetUp(t *testing.T) {
	reg := prometheus.NewRegistry()
	logger := zap.NewNop()

	metrics := NewCollectorMetrics(reg, logger)

	// Initially should be 0 (not set yet)
	metricFamilies, err := reg.Gather()
	require.NoError(t, err)

	var upValue float64
	for _, mf := range metricFamilies {
		if mf.GetName() == "netmon_loss_collector_up" {
			upValue = mf.GetMetric()[0].GetGauge().GetValue()
		}
	}
	assert.Equal(t, 0.0, upValue)

	// Set to up
	metrics.SetUp(true)

	metricFamilies, err = reg.Gather()
	require.NoError(t, err)

	for _, mf := range metricFamilies {
		if mf.GetName() == "netmon_loss_collector_up" {
			upValue = mf.GetMetric()[0].GetGauge().GetValue()
		}
	}
	assert.Equal(t, 1.0, upValue)

	// Set to down
	metrics.SetUp(false)

	metricFamilies, err = reg.Gather()
	require.NoError(t, err)

	for _, mf := range metricFamilies {
		if mf.GetName() == "netmon_loss_collector_up" {
			upValue = mf.GetMetric()[0].GetGauge().GetValue()
		}
	}
	assert.Equal(t, 0.0, upValue)
}

func TestCollectorMetrics_Counters(t *testing.T) {
	reg := prometheus.NewRegistry()
	logger := zap.NewNop()

	metrics := NewCollectorMetrics(reg, logger)

	// Increment counters
	metrics.IncEventsRead()
	metrics.IncEventsRead()
	metrics.IncEventsRead()

	metrics.IncEventsParsed()
	metrics.IncEventsParsed()

	metrics.IncParseErrors()

	// Gather and verify
	metricFamilies, err := reg.Gather()
	require.NoError(t, err)

	counters := make(map[string]float64)
	for _, mf := range metricFamilies {
		if mf.GetType().String() == "COUNTER" {
			counters[mf.GetName()] = mf.GetMetric()[0].GetCounter().GetValue()
		}
	}

	assert.Equal(t, 3.0, counters["netmon_loss_events_read_total"])
	assert.Equal(t, 2.0, counters["netmon_loss_events_parsed_total"])
	assert.Equal(t, 1.0, counters["netmon_loss_parse_errors_total"])
}

func TestCollectorMetrics_SourceInfo(t *testing.T) {
	reg := prometheus.NewRegistry()
	logger := zap.NewNop()

	metrics := NewCollectorMetrics(reg, logger)

	// Source info should be set to 1 on creation
	metricFamilies, err := reg.Gather()
	require.NoError(t, err)

	var sourceInfoValue float64
	found := false
	for _, mf := range metricFamilies {
		if mf.GetName() == "netmon_loss_source_info" {
			found = true
			sourceInfoValue = mf.GetMetric()[0].GetGauge().GetValue()
			// Check const labels
			labels := mf.GetMetric()[0].GetLabel()
			for _, label := range labels {
				if label.GetName() == "source" {
					assert.Equal(t, "trace_pipe", label.GetValue())
				}
			}
		}
	}
	require.True(t, found, "netmon_loss_source_info metric not found")
	assert.Equal(t, 1.0, sourceInfoValue)

	// Verify SetSourceInfo can be called again
	metrics.SetSourceInfo()
	metricFamilies, err = reg.Gather()
	require.NoError(t, err)
	for _, mf := range metricFamilies {
		if mf.GetName() == "netmon_loss_source_info" {
			sourceInfoValue = mf.GetMetric()[0].GetGauge().GetValue()
		}
	}
	assert.Equal(t, 1.0, sourceInfoValue)
}
