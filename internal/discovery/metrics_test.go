package discovery

import (
	"net"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func TestDiscoveryMetricsObserveAndCap(t *testing.T) {
	metrics := NewMetrics(prometheus.NewRegistry(), 1)
	path := &Path{SrcIP: net.ParseIP("10.0.0.1"), DstIP: net.ParseIP("10.0.0.2"), Hops: []Hop{
		{TTL: 1, RTT: time.Millisecond, ProbesSent: 3, ProbesReceived: 2, LossPercent: 100.0 / 3},
	}}
	metrics.Observe(path, &Bottleneck{HopIP: "192.0.2.1", LossPercent: 100.0 / 3})
	require.Equal(t, float64(1), testutil.ToFloat64(metrics.pathsActive))
	require.Equal(t, float64(1), testutil.ToFloat64(metrics.hops.WithLabelValues("10.0.0.1", "10.0.0.2")))
	require.InDelta(t, .001, testutil.ToFloat64(metrics.rtt.WithLabelValues("10.0.0.1", "10.0.0.2")), 1e-9)

	metrics.Observe(&Path{SrcIP: net.ParseIP("10.0.0.3"), DstIP: net.ParseIP("10.0.0.4")}, nil)
	require.Equal(t, float64(1), testutil.ToFloat64(metrics.dropped))
}
