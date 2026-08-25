package metadata

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	UnknownRole     = "role"
	UnknownLocation = "location"
	UnknownVRF      = "vrf"
)

var unknownAttributes = []string{UnknownRole, UnknownLocation, UnknownVRF}

// UnknownEntry describes one recently observed IP with incomplete metadata.
type UnknownEntry struct {
	IP         string    `json:"ip"`
	Missing    []string  `json:"missing"`
	Directions []string  `json:"directions"`
	FirstSeen  time.Time `json:"first_seen"`
	LastSeen   time.Time `json:"last_seen"`
	EventCount uint64    `json:"event_count"`
}

type unknownEntry struct {
	ip         string
	missing    map[string]bool
	directions map[string]bool
	firstSeen  time.Time
	lastSeen   time.Time
	eventCount uint64
}

// UnknownTracker maintains a TTL-bounded inventory independently of the loss
// metric label schema. It also implements prometheus.Collector for the
// dedicated high-cardinality endpoint.
type UnknownTracker struct {
	mu              sync.RWMutex
	locations       *LocationMatcher
	roles           *RoleMatcher
	ttl             time.Duration
	maxIPs          int
	entries         map[string]*unknownEntry
	attributeCounts map[string]int

	activeGauge  *prometheus.GaugeVec
	eventsTotal  *prometheus.CounterVec
	droppedTotal prometheus.Counter
	unknownDesc  *prometheus.Desc
}

// NewUnknownTracker registers only bounded aggregate metrics in reg. Register
// the tracker itself in a separate registry to expose per-IP diagnostics.
func NewUnknownTracker(
	locations *LocationMatcher,
	roles *RoleMatcher,
	ttl time.Duration,
	maxIPs int,
	reg prometheus.Registerer,
) *UnknownTracker {
	activeGauge := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "netmon_metadata_unknown_ips",
		Help: "Current number of recently observed distinct IPs with unresolved metadata",
	}, []string{"attribute"})
	eventsTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "netmon_metadata_unknown_events_total",
		Help: "Total endpoint observations whose metadata attribute was unresolved",
	}, []string{"attribute"})
	droppedTotal := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "netmon_metadata_unknown_observations_dropped_total",
		Help: "Total observations of new unknown IPs dropped because max_ips was reached",
	})
	reg.MustRegister(activeGauge, eventsTotal, droppedTotal)

	t := &UnknownTracker{
		locations:       locations,
		roles:           roles,
		ttl:             ttl,
		maxIPs:          maxIPs,
		entries:         make(map[string]*unknownEntry),
		attributeCounts: make(map[string]int, len(unknownAttributes)),
		activeGauge:     activeGauge,
		eventsTotal:     eventsTotal,
		droppedTotal:    droppedTotal,
		unknownDesc: prometheus.NewDesc(
			"netmon_metadata_unknown_ip",
			"Recently observed IP address with an unresolved metadata attribute",
			[]string{"ip", "attribute"}, nil,
		),
	}
	for _, attribute := range unknownAttributes {
		t.activeGauge.WithLabelValues(attribute).Set(0)
		t.eventsTotal.WithLabelValues(attribute)
	}
	return t
}

// ObservePair records both endpoints before the loss event is aggregated.
func (t *UnknownTracker) ObservePair(srcIP, dstIP string) {
	t.observeIP(srcIP, "src")
	t.observeIP(dstIP, "dst")
}

func (t *UnknownTracker) observeIP(ip, direction string) {
	if net.ParseIP(ip) == nil {
		return
	}
	missing := t.missingFor(ip)
	now := time.Now()

	t.mu.Lock()
	defer t.mu.Unlock()

	entry, exists := t.entries[ip]
	if len(missing) == 0 {
		if exists {
			t.deleteEntryLocked(ip, entry)
		}
		return
	}
	if !exists {
		if len(t.entries) >= t.maxIPs {
			t.droppedTotal.Inc()
			return
		}
		entry = &unknownEntry{
			ip:         ip,
			missing:    make(map[string]bool, len(missing)),
			directions: make(map[string]bool, 2),
			firstSeen:  now,
		}
		t.entries[ip] = entry
	}
	t.setMissingLocked(entry, missing)
	entry.directions[direction] = true
	entry.lastSeen = now
	entry.eventCount++
	for _, attribute := range missing {
		t.eventsTotal.WithLabelValues(attribute).Inc()
	}
}

func (t *UnknownTracker) missingFor(ip string) []string {
	missing := make([]string, 0, len(unknownAttributes))
	location := t.locations.Lookup(ip)
	if t.roles.GetRole(ip) == "unknown" {
		missing = append(missing, UnknownRole)
	}
	if location.Location == "unknown" {
		missing = append(missing, UnknownLocation)
	}
	if location.VRF == "unknown" {
		missing = append(missing, UnknownVRF)
	}
	return missing
}

func (t *UnknownTracker) setMissingLocked(entry *unknownEntry, missing []string) {
	next := make(map[string]bool, len(missing))
	for _, attribute := range missing {
		next[attribute] = true
		if !entry.missing[attribute] {
			t.attributeCounts[attribute]++
			t.activeGauge.WithLabelValues(attribute).Set(float64(t.attributeCounts[attribute]))
		}
	}
	for attribute := range entry.missing {
		if !next[attribute] {
			t.attributeCounts[attribute]--
			t.activeGauge.WithLabelValues(attribute).Set(float64(t.attributeCounts[attribute]))
		}
	}
	entry.missing = next
}

func (t *UnknownTracker) deleteEntryLocked(ip string, entry *unknownEntry) {
	for attribute := range entry.missing {
		t.attributeCounts[attribute]--
		t.activeGauge.WithLabelValues(attribute).Set(float64(t.attributeCounts[attribute]))
	}
	delete(t.entries, ip)
}

// Reconcile removes resolved addresses and refreshes partial metadata after a
// locations or roles reload.
func (t *UnknownTracker) Reconcile() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for ip, entry := range t.entries {
		missing := t.missingFor(ip)
		if len(missing) == 0 {
			t.deleteEntryLocked(ip, entry)
			continue
		}
		t.setMissingLocked(entry, missing)
	}
}

// Cleanup expires entries that have not been observed within the TTL.
func (t *UnknownTracker) Cleanup(now time.Time) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	removed := 0
	for ip, entry := range t.entries {
		if now.Sub(entry.lastSeen) > t.ttl {
			t.deleteEntryLocked(ip, entry)
			removed++
		}
	}
	return removed
}

// StartJanitor runs TTL cleanup until ctx is cancelled.
func (t *UnknownTracker) StartJanitor(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case now := <-ticker.C:
				t.Cleanup(now)
			case <-ctx.Done():
				return
			}
		}
	}()
}

// Describe implements prometheus.Collector for the dedicated registry.
func (t *UnknownTracker) Describe(ch chan<- *prometheus.Desc) {
	ch <- t.unknownDesc
}

// Collect implements prometheus.Collector for the dedicated registry.
func (t *UnknownTracker) Collect(ch chan<- prometheus.Metric) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	for _, entry := range t.entries {
		for attribute := range entry.missing {
			ch <- prometheus.MustNewConstMetric(
				t.unknownDesc, prometheus.GaugeValue, 1, entry.ip, attribute,
			)
		}
	}
}

// APIHandler returns the JSON inventory endpoint.
func (t *UnknownTracker) APIHandler() http.Handler {
	return http.HandlerFunc(t.handleAPI)
}

func (t *UnknownTracker) handleAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	attribute := r.URL.Query().Get("attribute")
	if attribute != "" && attribute != UnknownRole && attribute != UnknownLocation && attribute != UnknownVRF {
		http.Error(w, "attribute must be role, location, or vrf", http.StatusBadRequest)
		return
	}
	limit := 1000
	if t.maxIPs < limit {
		limit = t.maxIPs
	}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > t.maxIPs {
			http.Error(w, "limit must be a positive integer not exceeding max_ips", http.StatusBadRequest)
			return
		}
		limit = parsed
	}

	entries := t.snapshot(attribute)
	total := len(entries)
	if len(entries) > limit {
		entries = entries[:limit]
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(struct {
		Entries   []UnknownEntry `json:"entries"`
		Total     int            `json:"total"`
		Limit     int            `json:"limit"`
		Truncated bool           `json:"truncated"`
	}{Entries: entries, Total: total, Limit: limit, Truncated: total > limit})
}

func (t *UnknownTracker) snapshot(attribute string) []UnknownEntry {
	t.mu.RLock()
	defer t.mu.RUnlock()
	entries := make([]UnknownEntry, 0, len(t.entries))
	for _, entry := range t.entries {
		if attribute != "" && !entry.missing[attribute] {
			continue
		}
		missing := sortedKeys(entry.missing)
		directions := sortedKeys(entry.directions)
		entries = append(entries, UnknownEntry{
			IP:         entry.ip,
			Missing:    missing,
			Directions: directions,
			FirstSeen:  entry.firstSeen,
			LastSeen:   entry.lastSeen,
			EventCount: entry.eventCount,
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].IP < entries[j].IP })
	return entries
}

func sortedKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for value := range values {
		keys = append(keys, value)
	}
	sort.Strings(keys)
	return keys
}
