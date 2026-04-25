package metrics

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/vponomarev/network-monitor/internal/metadata"
	"github.com/vponomarev/network-monitor/internal/topology"
	"go.uber.org/zap"
)

// Exporter exports TCP retransmit metrics to Prometheus
type Exporter struct {
	mu              sync.RWMutex
	metricName      string
	counter         *prometheus.CounterVec
	locationMatcher *metadata.LocationMatcher
	roleMatcher     *metadata.RoleMatcher
	topology        *topology.Topology
	logger          *zap.Logger
	ttl             time.Duration

	// Internal tracking for TTL
	events map[pairKey]*pairData
}

type pairKey struct {
	src string
	dst string
}

type pairData struct {
	count    uint64
	lastSeen time.Time
}

// NewExporter creates a new metrics exporter
func NewExporter(
	metricName string,
	locationMatcher *metadata.LocationMatcher,
	roleMatcher *metadata.RoleMatcher,
	logger *zap.Logger,
) *Exporter {
	return NewExporterWithRegistry(metricName, locationMatcher, roleMatcher, logger, prometheus.DefaultRegisterer)
}

// NewExporterWithRegistry creates a new metrics exporter with a custom registry
func NewExporterWithRegistry(
	metricName string,
	locationMatcher *metadata.LocationMatcher,
	roleMatcher *metadata.RoleMatcher,
	logger *zap.Logger,
	reg prometheus.Registerer,
) *Exporter {
	counter := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: metricName,
			Help: "Total number of TCP retransmissions by connection pair",
		},
		[]string{
			"src_ip",
			"dst_ip",
			"src_location",
			"dst_location",
			"src_role",
			"dst_role",
			"src_network",
			"dst_network",
		},
	)

	reg.MustRegister(counter)

	return &Exporter{
		metricName:      metricName,
		counter:         counter,
		locationMatcher: locationMatcher,
		roleMatcher:     roleMatcher,
		logger:          logger.Named("exporter"),
		ttl:             3 * time.Hour, // Default TTL
		events:          make(map[pairKey]*pairData),
	}
}

// RecordRetransmit records a single retransmit event
func (e *Exporter) RecordRetransmit(srcIP, dstIP string) {
	key := pairKey{src: srcIP, dst: dstIP}

	e.mu.Lock()
	defer e.mu.Unlock()

	if data, ok := e.events[key]; ok {
		data.count++
		data.lastSeen = time.Now()
	} else {
		e.events[key] = &pairData{
			count:    1,
			lastSeen: time.Now(),
		}
	}

	// Update Prometheus metric
	e.updateMetric(key)
}

// updateMetric updates the Prometheus counter for a pair
func (e *Exporter) updateMetric(key pairKey) {
	data := e.events[key]
	if data == nil {
		return
	}

	srcLocation := e.locationMatcher.GetLocation(key.src)
	dstLocation := e.locationMatcher.GetLocation(key.dst)
	srcRole := e.roleMatcher.GetRole(key.src)
	dstRole := e.roleMatcher.GetRole(key.dst)
	srcNetwork := getNetwork(key.src)
	dstNetwork := getNetwork(key.dst)

	e.counter.WithLabelValues(
		key.src,
		key.dst,
		srcLocation,
		dstLocation,
		srcRole,
		dstRole,
		srcNetwork,
		dstNetwork,
	).Add(float64(data.count))
}

// getNetwork returns the /24 network for an IP
func getNetwork(ip string) string {
	// Simple /24 network extraction
	// For production, use proper IP parsing
	parts := splitIP(ip)
	if len(parts) != 4 {
		return "0.0.0.0/24"
	}
	return parts[0] + "." + parts[1] + "." + parts[2] + ".0/24"
}

// splitIP splits an IP string into octets
func splitIP(ip string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(ip); i++ {
		if ip[i] == '.' {
			parts = append(parts, ip[start:i])
			start = i + 1
		}
	}
	parts = append(parts, ip[start:])
	return parts
}

// cleanupOld removes events older than TTL
func (e *Exporter) cleanupOld() {
	e.mu.Lock()
	defer e.mu.Unlock()

	now := time.Now()
	for key, data := range e.events {
		if now.Sub(data.lastSeen) > e.ttl {
			delete(e.events, key)
			e.logger.Debug("Cleaned up old event",
				zap.String("src", key.src),
				zap.String("dst", key.dst))
		}
	}
}

// SetTTL sets the TTL for events
func (e *Exporter) SetTTL(ttl time.Duration) {
	e.mu.Lock()
	e.ttl = ttl
	e.mu.Unlock()
}

// GetEventCount returns the number of tracked events (for testing)
func (e *Exporter) GetEventCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.events)
}

// Describe implements prometheus.Collector
func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	e.counter.Describe(ch)
}

// Collect implements prometheus.Collector
func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	e.cleanupOld()
	e.counter.Collect(ch)
}

// Collector returns the exporter as a prometheus.Collector for HTTP handler
func (e *Exporter) Collector() prometheus.Collector {
	return e
}

// SetMatchers updates the location and role matchers (for SIGHUP reload)
func (e *Exporter) SetMatchers(locationMatcher *metadata.LocationMatcher, roleMatcher *metadata.RoleMatcher) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.locationMatcher = locationMatcher
	e.roleMatcher = roleMatcher
	e.logger.Info("Matchers updated")
}

// SetTopology sets the network topology (for SIGHUP reload)
func (e *Exporter) SetTopology(topology *topology.Topology) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.topology = topology
	e.logger.Info("Topology updated")
}
