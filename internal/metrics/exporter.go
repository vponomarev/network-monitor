package metrics

import (
	"context"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/vponomarev/network-monitor/internal/metadata"
	"go.uber.org/zap"
)

// Cardinality levels control the label set of the loss metric and therefore the
// number of Prometheus series a busy host can produce.
const (
	// LevelIP labels every series with the full src_ip/dst_ip pair. Highest
	// fidelity, unbounded cardinality — only safe on small/known IP spaces.
	LevelIP = "ip"
	// LevelNetwork aggregates to /24 networks (no per-IP labels).
	LevelNetwork = "network"
	// LevelRole aggregates to location/role/vrf (no IP, no network). Default —
	// bounded cardinality suitable for production.
	LevelRole = "role"
)

// seriesKeySep separates label values when building the internal series key.
// Unit Separator (0x1f) never appears in IPs/roles/locations.
const seriesKeySep = "\x1f"

// CardinalityConfig controls the loss metric label granularity and the hard
// cap on the number of active series.
type CardinalityConfig struct {
	// Level is one of LevelIP, LevelNetwork, LevelRole.
	Level string
	// MaxSeries caps the number of distinct active series. 0 means unlimited.
	MaxSeries int
	// LabelNames is the exact configured label allowlist. A nil value preserves
	// the legacy level-based label set for programmatic callers.
	LabelNames []string
}

// defaultedLevel returns a valid level, falling back to LevelRole.
func (c CardinalityConfig) defaultedLevel() string {
	switch c.Level {
	case LevelIP, LevelNetwork, LevelRole:
		return c.Level
	default:
		return LevelRole
	}
}

// Exporter exports TCP retransmit metrics to Prometheus.
type Exporter struct {
	mu              sync.RWMutex
	metricName      string
	counter         *prometheus.CounterVec
	locationMatcher *metadata.LocationMatcher
	roleMatcher     *metadata.RoleMatcher
	registerer      prometheus.Registerer
	matcherVersion  uint64
	logger          *zap.Logger
	ttl             time.Duration

	// Cardinality controls label granularity and the series cap.
	level      string
	maxSeries  int
	labelNames []string

	// Cardinality observability.
	activeSeries  prometheus.Gauge
	seriesDropped prometheus.Counter
	lastDropLog   time.Time

	// Internal tracking, keyed by the series identity (joined label values).
	series map[string]*seriesData
}

// seriesData tracks one Prometheus series: its label values, accumulated count,
// last-seen time (for TTL) and a representative IP pair used to recompute labels
// on matcher reload.
type seriesData struct {
	labels   []string
	count    uint64
	lastSeen time.Time
	repSrc   string
	repDst   string
	counter  prometheus.Counter
}

// NewExporter creates a new metrics exporter on the default registry with the
// legacy IP-level, unbounded label set (kept for backward compatibility).
func NewExporter(
	metricName string,
	locationMatcher *metadata.LocationMatcher,
	roleMatcher *metadata.RoleMatcher,
	logger *zap.Logger,
) *Exporter {
	return NewExporterWithRegistry(metricName, locationMatcher, roleMatcher, logger, prometheus.DefaultRegisterer)
}

// NewExporterWithRegistry creates an exporter on a custom registry with the
// legacy IP-level, unbounded label set (kept for backward compatibility with
// existing callers and tests).
func NewExporterWithRegistry(
	metricName string,
	locationMatcher *metadata.LocationMatcher,
	roleMatcher *metadata.RoleMatcher,
	logger *zap.Logger,
	reg prometheus.Registerer,
) *Exporter {
	return NewExporterWithConfig(metricName, locationMatcher, roleMatcher, logger, reg,
		CardinalityConfig{Level: LevelIP, MaxSeries: 0})
}

// NewExporterWithConfig creates an exporter with an explicit cardinality config.
// This is the production constructor (see cmd/netmon/main.go).
func NewExporterWithConfig(
	metricName string,
	locationMatcher *metadata.LocationMatcher,
	roleMatcher *metadata.RoleMatcher,
	logger *zap.Logger,
	reg prometheus.Registerer,
	card CardinalityConfig,
) *Exporter {
	level := card.defaultedLevel()
	labelNames := labelNamesForLevel(level, card.LabelNames)

	counter := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: metricName,
			Help: "Total number of TCP retransmissions by connection pair",
		},
		labelNames,
	)

	activeSeries := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "netmon_loss_active_series",
		Help: "Current number of active TCP loss series being exported",
	})
	seriesDropped := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "netmon_loss_series_dropped_total",
		Help: "Total number of loss events dropped because the max_series cap was reached",
	})

	reg.MustRegister(counter, activeSeries, seriesDropped)

	logger.Named("exporter").Info("Loss exporter cardinality configured",
		zap.String("level", level),
		zap.Int("max_series", card.MaxSeries),
		zap.Strings("labels", labelNames))

	return &Exporter{
		metricName:      metricName,
		counter:         counter,
		locationMatcher: locationMatcher,
		roleMatcher:     roleMatcher,
		registerer:      reg,
		logger:          logger.Named("exporter"),
		ttl:             3 * time.Hour, // Default TTL
		level:           level,
		maxSeries:       card.MaxSeries,
		labelNames:      labelNames,
		activeSeries:    activeSeries,
		seriesDropped:   seriesDropped,
		series:          make(map[string]*seriesData),
	}
}

// labelNamesForLevel applies the cardinality level as an upper bound to the
// configured label allowlist. A nil allowlist preserves the legacy label set
// for programmatic callers that predate configurable labels.
func labelNamesForLevel(level string, configured []string) []string {
	if configured != nil {
		allowed := allowedLabelsForLevel(level)
		seen := make(map[string]struct{}, len(configured))
		labels := make([]string, 0, len(configured))
		for _, label := range configured {
			if !allowed[label] {
				continue
			}
			if _, ok := seen[label]; ok {
				continue
			}
			seen[label] = struct{}{}
			labels = append(labels, label)
		}
		return labels
	}

	switch level {
	case LevelNetwork:
		return []string{
			"src_network", "dst_network",
			"src_location", "dst_location",
			"src_role", "dst_role",
			"src_vrf", "dst_vrf",
		}
	case LevelRole:
		return []string{
			"src_location", "dst_location",
			"src_role", "dst_role",
			"src_vrf", "dst_vrf",
		}
	default: // LevelIP
		return []string{
			"src_ip", "dst_ip",
			"src_location", "dst_location",
			"src_role", "dst_role",
			"src_network", "dst_network",
			"src_vrf", "dst_vrf",
		}
	}
}

func allowedLabelsForLevel(level string) map[string]bool {
	allowed := map[string]bool{
		"src_location": true,
		"dst_location": true,
		"src_role":     true,
		"dst_role":     true,
		"src_vrf":      true,
		"dst_vrf":      true,
	}
	if level == LevelNetwork || level == LevelIP {
		allowed["src_network"] = true
		allowed["dst_network"] = true
	}
	if level == LevelIP {
		allowed["src_ip"] = true
		allowed["dst_ip"] = true
	}
	return allowed
}

// labelsFor computes only the configured label values in the order matching
// e.labelNames.
func (e *Exporter) labelsFor(src, dst string) []string {
	return e.labelsForWithMatchers(src, dst, e.locationMatcher, e.roleMatcher)
}

func (e *Exporter) labelsForWithMatchers(src, dst string, locationMatcher *metadata.LocationMatcher, roleMatcher *metadata.RoleMatcher) []string {
	var srcLocation, dstLocation metadata.LocationMetadata
	locationsLoaded := false
	loadLocations := func() {
		if !locationsLoaded {
			srcLocation = locationMatcher.Lookup(src)
			dstLocation = locationMatcher.Lookup(dst)
			locationsLoaded = true
		}
	}
	var srcRole, dstRole string
	rolesLoaded := false
	loadRoles := func() {
		if !rolesLoaded {
			srcRole = roleMatcher.GetRole(src)
			dstRole = roleMatcher.GetRole(dst)
			rolesLoaded = true
		}
	}
	values := make([]string, 0, len(e.labelNames))
	for _, label := range e.labelNames {
		switch label {
		case "src_ip":
			values = append(values, src)
		case "dst_ip":
			values = append(values, dst)
		case "src_location":
			loadLocations()
			values = append(values, srcLocation.Location)
		case "dst_location":
			loadLocations()
			values = append(values, dstLocation.Location)
		case "src_role":
			loadRoles()
			values = append(values, srcRole)
		case "dst_role":
			loadRoles()
			values = append(values, dstRole)
		case "src_network":
			values = append(values, getNetwork(src))
		case "dst_network":
			values = append(values, getNetwork(dst))
		case "src_vrf":
			loadLocations()
			values = append(values, srcLocation.VRF)
		case "dst_vrf":
			loadLocations()
			values = append(values, dstLocation.VRF)
		}
	}
	return values
}

func resolveEndpoint(ip string, locationMatcher *metadata.LocationMatcher, roleMatcher *metadata.RoleMatcher) metadata.EndpointMetadata {
	return metadata.EndpointMetadata{
		Location: locationMatcher.Lookup(ip),
		Role:     roleMatcher.GetRole(ip),
	}
}

func (e *Exporter) labelsForResolved(srcIP, dstIP string, src, dst metadata.EndpointMetadata) []string {
	values := make([]string, 0, len(e.labelNames))
	for _, label := range e.labelNames {
		switch label {
		case "src_ip":
			values = append(values, srcIP)
		case "dst_ip":
			values = append(values, dstIP)
		case "src_location":
			values = append(values, src.Location.Location)
		case "dst_location":
			values = append(values, dst.Location.Location)
		case "src_role":
			values = append(values, src.Role)
		case "dst_role":
			values = append(values, dst.Role)
		case "src_network":
			values = append(values, getNetwork(srcIP))
		case "dst_network":
			values = append(values, getNetwork(dstIP))
		case "src_vrf":
			values = append(values, src.Location.VRF)
		case "dst_vrf":
			values = append(values, dst.Location.VRF)
		}
	}
	return values
}

// seriesKeyOf builds the internal map key identifying a series.
func seriesKeyOf(labels []string) string {
	return strings.Join(labels, seriesKeySep)
}

// RecordRetransmit records a single retransmit event.
func (e *Exporter) RecordRetransmit(srcIP, dstIP string) {
	// Metadata lookup can be much more expensive than the counter update. Keep
	// it outside the exporter write lock so independent ring-buffer events do
	// not serialize on linear prefix scans.
	e.mu.RLock()
	locationMatcher, roleMatcher, matcherVersion := e.locationMatcher, e.roleMatcher, e.matcherVersion
	e.mu.RUnlock()
	labels := e.labelsForWithMatchers(srcIP, dstIP, locationMatcher, roleMatcher)

	e.mu.Lock()
	defer e.mu.Unlock()
	if matcherVersion != e.matcherVersion {
		// A reload raced with the lookup. Recompute against the new matchers while
		// holding the lock; this is rare and prevents resurrecting stale labels.
		labels = e.labelsFor(srcIP, dstIP)
	}
	e.recordRetransmitLocked(srcIP, dstIP, labels)
}

// RecordRetransmitResolved records an event and returns the endpoint metadata
// used for its labels. Callers can reuse it for auxiliary diagnostics.
func (e *Exporter) RecordRetransmitResolved(srcIP, dstIP string) (metadata.EndpointMetadata, metadata.EndpointMetadata) {
	e.mu.RLock()
	locationMatcher, roleMatcher, matcherVersion := e.locationMatcher, e.roleMatcher, e.matcherVersion
	e.mu.RUnlock()
	src := resolveEndpoint(srcIP, locationMatcher, roleMatcher)
	dst := resolveEndpoint(dstIP, locationMatcher, roleMatcher)
	labels := e.labelsForResolved(srcIP, dstIP, src, dst)

	e.mu.Lock()
	defer e.mu.Unlock()
	if matcherVersion != e.matcherVersion {
		src = resolveEndpoint(srcIP, e.locationMatcher, e.roleMatcher)
		dst = resolveEndpoint(dstIP, e.locationMatcher, e.roleMatcher)
		labels = e.labelsForResolved(srcIP, dstIP, src, dst)
	}
	e.recordRetransmitLocked(srcIP, dstIP, labels)
	return src, dst
}

// recordRetransmitLocked updates the bounded series map. Caller holds e.mu.
func (e *Exporter) recordRetransmitLocked(srcIP, dstIP string, labels []string) {
	key := seriesKeyOf(labels)

	data, ok := e.series[key]
	if !ok {
		// New series — enforce the cardinality cap before creating it.
		if e.maxSeries > 0 && len(e.series) >= e.maxSeries {
			e.seriesDropped.Inc()
			e.logDropRateLimited(srcIP, dstIP)
			return
		}
		data = &seriesData{
			labels:  labels,
			repSrc:  srcIP,
			repDst:  dstIP,
			counter: e.counter.WithLabelValues(labels...),
		}
		e.series[key] = data
		e.activeSeries.Set(float64(len(e.series)))
	}

	data.count++
	data.lastSeen = time.Now()

	// Increment the Prometheus counter by exactly 1 per event.
	data.counter.Inc()
}

// logDropRateLimited logs a max_series overflow at most once per 30s. Caller
// must hold e.mu.
func (e *Exporter) logDropRateLimited(srcIP, dstIP string) {
	now := time.Now()
	if now.Sub(e.lastDropLog) < 30*time.Second {
		return
	}
	e.lastDropLog = now
	e.logger.Warn("max_series cap reached — dropping new loss series",
		zap.Int("max_series", e.maxSeries),
		zap.Int("active_series", len(e.series)),
		zap.String("dropped_src", srcIP),
		zap.String("dropped_dst", dstIP))
}

// getNetwork returns a bounded-cardinality network for IPv4 or IPv6.
func getNetwork(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return "unknown"
	}
	bits := 64
	if addr.Is4() {
		bits = 24
	}
	return netip.PrefixFrom(addr, bits).Masked().String()
}

// cleanupOld removes series older than TTL and deletes them from the CounterVec.
func (e *Exporter) cleanupOld() {
	e.mu.Lock()
	defer e.mu.Unlock()

	now := time.Now()
	removed := 0
	for key, data := range e.series {
		if now.Sub(data.lastSeen) > e.ttl {
			if len(data.labels) > 0 {
				if !e.counter.DeleteLabelValues(data.labels...) {
					e.logger.Debug("Failed to delete label values from counter",
						zap.String("src", data.repSrc),
						zap.String("dst", data.repDst))
				}
			}
			delete(e.series, key)
			removed++
		}
	}
	if removed > 0 {
		e.activeSeries.Set(float64(len(e.series)))
	}
}

// StartJanitor runs periodic TTL cleanup until ctx is cancelled. It is required
// in production because the exporter registers the raw CounterVec (not itself)
// with the registry, so Collect — and thus cleanupOld — is never triggered by
// scrapes. Call once from main.
func (e *Exporter) StartJanitor(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				e.cleanupOld()
			}
		}
	}()
}

// SetTTL sets the TTL for events.
func (e *Exporter) SetTTL(ttl time.Duration) {
	e.mu.Lock()
	e.ttl = ttl
	e.mu.Unlock()
}

// GetEventCount returns the number of active series (for testing).
func (e *Exporter) GetEventCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.series)
}

// Describe implements prometheus.Collector.
func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	e.counter.Describe(ch)
}

// Collect implements prometheus.Collector.
func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	e.cleanupOld()
	e.counter.Collect(ch)
}

// Collector returns the exporter as a prometheus.Collector for HTTP handler.
func (e *Exporter) Collector() prometheus.Collector {
	return e
}

// SetMatchers updates the location and role matchers (for SIGHUP reload) and
// begins a new counter epoch; historical aggregates cannot be relabelled safely.
func (e *Exporter) SetMatchers(locationMatcher *metadata.LocationMatcher, roleMatcher *metadata.RoleMatcher) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Aggregates cannot be split by a representative IP. Start a new counter
	// epoch on reload; Prometheus treats the decrease as a counter reset.
	e.locationMatcher = locationMatcher
	e.roleMatcher = roleMatcher
	e.matcherVersion++
	e.resetMetadataEpoch()
}

// ReplaceMetadata commits staged matcher contents under the same lock as the
// counter epoch. A lookup started before the commit is retried using its version.
func (e *Exporter) ReplaceMetadata(locations *metadata.LocationMatcher, roles *metadata.RoleMatcher) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if locations != nil {
		e.locationMatcher.ReplaceFrom(locations)
	}
	if roles != nil {
		e.roleMatcher.ReplaceFrom(roles)
	}
	e.matcherVersion++
	e.resetMetadataEpoch()
}

func (e *Exporter) resetMetadataEpoch() {
	e.counter.Reset()
	e.series = make(map[string]*seriesData)
	e.activeSeries.Set(0)
	e.logger.Info("Matchers updated; loss counters reset for the new metadata epoch")
}

// Registry returns the Prometheus registerer used by the exporter.
func (e *Exporter) Registry() prometheus.Registerer {
	return e.registerer
}
