package conntrack

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestMetricsCollectorsUseInjectedRegistries(t *testing.T) {
	first := NewMetricsCollectorWithRegisterer(zap.NewNop(), prometheus.NewRegistry())
	second := NewMetricsCollectorWithRegisterer(zap.NewNop(), prometheus.NewRegistry())
	require.NotNil(t, first)
	require.NotNil(t, second)
}

func TestUpdateDroppedMetricsConvertsAbsoluteValuesToCounterDeltas(t *testing.T) {
	collector := NewMetricsCollectorWithRegisterer(zap.NewNop(), prometheus.NewRegistry())
	metric := collector.droppedEventsTotal.WithLabelValues("ringbuf_full")

	collector.UpdateDroppedMetrics("ringbuf_full", 10)
	collector.UpdateDroppedMetrics("ringbuf_full", 13)
	require.Equal(t, float64(13), testutil.ToFloat64(metric))

	// A source reset contributes its new absolute value, preserving monotonicity.
	collector.UpdateDroppedMetrics("ringbuf_full", 2)
	require.Equal(t, float64(15), testutil.ToFloat64(metric))
}
