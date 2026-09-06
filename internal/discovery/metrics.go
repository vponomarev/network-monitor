package discovery

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Metrics exports bounded path-discovery state. Path labels are capped to avoid
// turning the on-demand API into an unbounded Prometheus cardinality source.
type Metrics struct {
	mu          sync.Mutex
	maxPaths    int
	paths       map[string]string
	updated     map[string]time.Time
	pathsActive prometheus.Gauge
	lastRun     prometheus.Gauge
	hops        *prometheus.GaugeVec
	rtt         *prometheus.GaugeVec
	bottleneck  *prometheus.GaugeVec
	dropped     prometheus.Counter
}

// StartJanitor removes stale series even when no new traces arrive.
func (m *Metrics) StartJanitor(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				m.mu.Lock()
				for key, seen := range m.updated {
					if now.Sub(seen) >= 10*time.Minute {
						m.remove(key)
					}
				}
				m.mu.Unlock()
			}
		}
	}()
}

func NewMetrics(reg prometheus.Registerer, maxPaths int) *Metrics {
	if maxPaths < 1 {
		maxPaths = 1000
	}
	m := &Metrics{
		maxPaths: maxPaths,
		paths:    make(map[string]string),
		updated:  make(map[string]time.Time),
		pathsActive: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "netmon", Name: "discovery_paths", Help: "Number of paths currently exported by discovery.",
		}),
		lastRun: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "netmon", Name: "discovery_last_run_timestamp_seconds", Help: "Unix timestamp of the last successful discovery.",
		}),
		hops: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "netmon", Name: "path_hops", Help: "Number of hops in a discovered path.",
		}, []string{"src_ip", "dst_ip"}),
		rtt: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "netmon", Name: "path_rtt_seconds", Help: "Average RTT of responding hops in a discovered path.",
		}, []string{"src_ip", "dst_ip"}),
		bottleneck: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "netmon", Name: "path_bottleneck_loss_percent", Help: "Estimated loss percentage at the path bottleneck.",
		}, []string{"src_ip", "dst_ip", "bottleneck_ip"}),
		dropped: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "netmon", Name: "discovery_metric_series_dropped_total", Help: "Path metric series rejected by the cardinality cap.",
		}),
	}
	if reg != nil {
		reg.MustRegister(m.pathsActive, m.lastRun, m.hops, m.rtt, m.bottleneck, m.dropped)
	}
	return m
}

func (m *Metrics) Observe(path *Path, bottleneck *Bottleneck) {
	if m == nil || path == nil {
		return
	}
	src, dst := path.SrcIP.String(), path.DstIP.String()
	key := src + "\x00" + dst
	m.mu.Lock()
	now := time.Now()
	for old, seen := range m.updated {
		if now.Sub(seen) >= 10*time.Minute {
			m.remove(old)
		}
	}
	previousBottleneck, ok := m.paths[key]
	if !ok {
		if len(m.paths) >= m.maxPaths {
			oldest := ""
			for candidate, seen := range m.updated {
				if oldest == "" || seen.Before(m.updated[oldest]) {
					oldest = candidate
				}
			}
			m.remove(oldest)
			m.dropped.Inc()
		}
		m.paths[key] = ""
		m.pathsActive.Set(float64(len(m.paths)))
	}
	m.updated[key] = now
	m.hops.WithLabelValues(src, dst).Set(float64(len(path.Hops)))
	m.rtt.WithLabelValues(src, dst).Set(path.AvgRTT().Seconds())
	if previousBottleneck != "" && (bottleneck == nil || previousBottleneck != bottleneck.HopIP) {
		m.bottleneck.DeleteLabelValues(src, dst, previousBottleneck)
	}
	if bottleneck != nil {
		m.bottleneck.WithLabelValues(src, dst, bottleneck.HopIP).Set(bottleneck.LossPercent)
		m.paths[key] = bottleneck.HopIP
	} else {
		m.paths[key] = ""
	}
	m.lastRun.SetToCurrentTime()
	m.mu.Unlock()
}

func (m *Metrics) remove(key string) {
	labels := strings.Split(key, "\x00")
	if len(labels) != 2 {
		return
	}
	m.hops.DeleteLabelValues(labels...)
	m.rtt.DeleteLabelValues(labels...)
	m.bottleneck.DeleteLabelValues(labels[0], labels[1], m.paths[key])
	delete(m.paths, key)
	delete(m.updated, key)
	m.pathsActive.Set(float64(len(m.paths)))
}
