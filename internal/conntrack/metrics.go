package conntrack

import (
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

// MetricsCollector collects and exports connection tracking metrics
type MetricsCollector struct {
	logger *zap.Logger

	// Connection state metrics
	connectionsTotal    *prometheus.GaugeVec
	eventsTotal         *prometheus.CounterVec
	handshakeSeconds    *prometheus.HistogramVec
	connectionDuration  *prometheus.HistogramVec
	bytesTransferred    *prometheus.CounterVec
	bytesPerConnection  *prometheus.HistogramVec
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector(logger *zap.Logger) *MetricsCollector {
	mc := &MetricsCollector{
		logger: logger.Named("conntrack_metrics"),
	}

	// Connection states gauge
	mc.connectionsTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "conntrack_connections",
			Help: "Number of connections by state and direction",
		},
		[]string{"state", "direction"},
	)

	// Events counter
	mc.eventsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "conntrack_events_total",
			Help: "Total number of connection events",
		},
		[]string{"event", "direction"},
	)

	// Handshake duration histogram
	mc.handshakeSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "conntrack_handshake_duration_seconds",
			Help:    "TCP handshake duration in seconds",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 10), // 1ms to 512ms
		},
		[]string{"direction"},
	)

	// Connection duration histogram
	mc.connectionDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "conntrack_connection_duration_seconds",
			Help:    "Total connection duration in seconds",
			Buckets: prometheus.ExponentialBuckets(1, 2, 15), // 1s to 32768s (~9h)
		},
		[]string{"direction"},
	)

	// Bytes transferred counter
	mc.bytesTransferred = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "conntrack_bytes_total",
			Help: "Total bytes transferred by direction",
		},
		[]string{"direction", "type"}, // type: sent or received
	)

	// Bytes per connection histogram
	mc.bytesPerConnection = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "conntrack_bytes_per_connection",
			Help:    "Bytes transferred per connection",
			Buckets: prometheus.ExponentialBuckets(100, 10, 8), // 100B to 10GB
		},
		[]string{"direction"},
	)

	// Register metrics
	prometheus.MustRegister(mc.connectionsTotal)
	prometheus.MustRegister(mc.eventsTotal)
	prometheus.MustRegister(mc.handshakeSeconds)
	prometheus.MustRegister(mc.connectionDuration)
	prometheus.MustRegister(mc.bytesTransferred)
	prometheus.MustRegister(mc.bytesPerConnection)

	return mc
}

// OnConnectionEvent handles connection events for metrics
func (mc *MetricsCollector) OnConnectionEvent(conn *Connection, event ConnectionEvent) {
	direction := conn.Direction.String()

	// Count events
	mc.eventsTotal.WithLabelValues(event.String(), direction).Inc()

	// Track handshake duration for established connections
	if event == EventEstablished {
		if hs := conn.HandshakeDuration(); hs > 0 {
			mc.handshakeSeconds.WithLabelValues(direction).Observe(hs.Seconds())
		}
	}

	// Track bytes transferred
	if conn.BytesSent > 0 {
		mc.bytesTransferred.WithLabelValues(direction, "sent").Add(float64(conn.BytesSent))
	}
	if conn.BytesRecv > 0 {
		mc.bytesTransferred.WithLabelValues(direction, "received").Add(float64(conn.BytesRecv))
	}

	// Track connection duration and bytes for closed connections
	if event == EventClosed || event == EventFailed || event == EventRejected {
		mc.connectionDuration.WithLabelValues(direction).Observe(conn.Duration().Seconds())
		
		totalBytes := conn.BytesSent + conn.BytesRecv
		if totalBytes > 0 {
			mc.bytesPerConnection.WithLabelValues(direction).Observe(float64(totalBytes))
		}
	}
}

// UpdateStateMetrics updates connection state metrics
func (mc *MetricsCollector) UpdateStateMetrics(stats Stats) {
	mc.connectionsTotal.WithLabelValues("pending_outgoing", "outgoing").Set(float64(stats.PendingOutgoing))
	mc.connectionsTotal.WithLabelValues("pending_incoming", "incoming").Set(float64(stats.PendingIncoming))
	mc.connectionsTotal.WithLabelValues("established", "").Set(float64(stats.Established))
}

// Stop unregisters metrics
func (mc *MetricsCollector) Stop() {
	prometheus.Unregister(mc.connectionsTotal)
	prometheus.Unregister(mc.eventsTotal)
	prometheus.Unregister(mc.handshakeSeconds)
	prometheus.Unregister(mc.connectionDuration)
	prometheus.Unregister(mc.bytesTransferred)
	prometheus.Unregister(mc.bytesPerConnection)
}
