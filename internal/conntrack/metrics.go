package conntrack

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

// MetricsCollector collects and exports connection tracking metrics
type MetricsCollector struct {
	logger      *zap.Logger
	reg         prometheus.Registerer
	dropMu      sync.Mutex
	lastDropped map[string]uint64

	// Connection state metrics
	connectionsTotal   *prometheus.GaugeVec
	eventsTotal        *prometheus.CounterVec
	handshakeSeconds   *prometheus.HistogramVec
	connectionDuration *prometheus.HistogramVec
	droppedEventsTotal *prometheus.CounterVec
	stateEntries       *prometheus.GaugeVec
	stateCleanup       *prometheus.CounterVec
	stateEvictions     *prometheus.CounterVec
	stateOverflows     *prometheus.CounterVec
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector(logger *zap.Logger) *MetricsCollector {
	return NewMetricsCollectorWithRegisterer(logger, prometheus.DefaultRegisterer)
}

func NewMetricsCollectorWithRegisterer(logger *zap.Logger, reg prometheus.Registerer) *MetricsCollector {
	if reg == nil {
		reg = prometheus.NewRegistry()
	}
	mc := &MetricsCollector{
		logger:      logger.Named("conntrack_metrics"),
		reg:         reg,
		lastDropped: make(map[string]uint64),
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

	// Sources report absolute monotonically increasing values. Convert their
	// deltas into a real Prometheus counter.
	mc.droppedEventsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "conntrack_dropped_events_total",
			Help: "Total number of dropped connection events by reason",
		},
		[]string{"reason"},
	)
	mc.stateEntries = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "conntrack_state_entries",
			Help: "Current conntrack state entries by storage layer",
		},
		[]string{"layer"},
	)
	mc.stateCleanup = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "conntrack_state_cleanup_total",
			Help: "Conntrack state entries removed by cleanup reason",
		},
		[]string{"reason"},
	)
	mc.stateEvictions = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "conntrack_state_evictions_total",
			Help: "Conntrack state entries evicted to enforce a hard limit",
		},
		[]string{"layer"},
	)
	mc.stateOverflows = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "conntrack_state_overflow_total",
			Help: "Conntrack state insertions that encountered a full bounded store",
		},
		[]string{"layer"},
	)

	// Register metrics
	reg.MustRegister(mc.connectionsTotal, mc.eventsTotal, mc.handshakeSeconds,
		mc.connectionDuration, mc.droppedEventsTotal, mc.stateEntries,
		mc.stateCleanup, mc.stateEvictions, mc.stateOverflows)
	for _, reason := range []string{"event_channel_full", "syslog_queue_full", "ringbuf_full", "connections_map_full", "pending_map_full"} {
		mc.droppedEventsTotal.WithLabelValues(reason).Add(0)
	}
	for _, layer := range []string{"userspace", "kernel_connections", "kernel_pending"} {
		mc.stateEntries.WithLabelValues(layer).Set(0)
		mc.stateEvictions.WithLabelValues(layer).Add(0)
		mc.stateOverflows.WithLabelValues(layer).Add(0)
	}
	for _, reason := range []string{
		CleanupReasonClosed,
		CleanupReasonTTL,
		CleanupReasonSYNTimeout,
		"ttl_kernel_connections",
		"ttl_kernel_pending",
	} {
		mc.stateCleanup.WithLabelValues(reason).Add(0)
	}

	return mc
}

func (mc *MetricsCollector) UpdateStateEntries(layer string, count int) {
	mc.stateEntries.WithLabelValues(layer).Set(float64(count))
}

func (mc *MetricsCollector) AddCleanup(reason string, count int) {
	if count > 0 {
		mc.stateCleanup.WithLabelValues(reason).Add(float64(count))
	}
}

func (mc *MetricsCollector) AddEviction(layer string, count int) {
	if count > 0 {
		mc.stateEvictions.WithLabelValues(layer).Add(float64(count))
	}
}

func (mc *MetricsCollector) AddOverflow(layer string, count uint64) {
	if count > 0 {
		mc.stateOverflows.WithLabelValues(layer).Add(float64(count))
	}
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

	// Track connection duration for terminal events.
	if event == EventClosed || event == EventFailed || event == EventRejected {
		mc.connectionDuration.WithLabelValues(direction).Observe(conn.Duration().Seconds())
	}
}

// UpdateStateMetrics updates connection state metrics
func (mc *MetricsCollector) UpdateStateMetrics(stats Stats) {
	mc.connectionsTotal.WithLabelValues("pending_outgoing", "outgoing").Set(float64(stats.PendingOutgoing))
	mc.connectionsTotal.WithLabelValues("pending_incoming", "incoming").Set(float64(stats.PendingIncoming))
	mc.connectionsTotal.WithLabelValues("established", "outgoing").Set(float64(stats.EstablishedOutgoing))
	mc.connectionsTotal.WithLabelValues("established", "incoming").Set(float64(stats.EstablishedIncoming))
}

// UpdateDroppedMetrics updates the dropped events metric
func (mc *MetricsCollector) UpdateDroppedMetrics(reason string, dropped uint64) {
	mc.dropMu.Lock()
	previous := mc.lastDropped[reason]
	delta := dropped
	if dropped >= previous {
		delta = dropped - previous
	}
	mc.lastDropped[reason] = dropped
	mc.dropMu.Unlock()
	if delta > 0 {
		mc.droppedEventsTotal.WithLabelValues(reason).Add(float64(delta))
	}
}

// Stop unregisters metrics
func (mc *MetricsCollector) Stop() {
	mc.reg.Unregister(mc.connectionsTotal)
	mc.reg.Unregister(mc.eventsTotal)
	mc.reg.Unregister(mc.handshakeSeconds)
	mc.reg.Unregister(mc.connectionDuration)
	mc.reg.Unregister(mc.droppedEventsTotal)
	mc.reg.Unregister(mc.stateEntries)
	mc.reg.Unregister(mc.stateCleanup)
	mc.reg.Unregister(mc.stateEvictions)
	mc.reg.Unregister(mc.stateOverflows)
}
