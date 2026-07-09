package collector

import (
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

// CollectorMetrics holds self-observation metrics for the loss collector
type CollectorMetrics struct {
	up           prometheus.Gauge
	eventsRead   prometheus.Counter
	eventsParsed prometheus.Counter
	parseErrors  prometheus.Counter
	sourceInfo   prometheus.Gauge
}

// NewCollectorMetrics creates and registers collector metrics. An optional
// source label ("trace_pipe" or "ebpf") sets netmon_loss_source_info; it
// defaults to "trace_pipe" when omitted (backward compatible).
func NewCollectorMetrics(reg prometheus.Registerer, logger *zap.Logger, source ...string) *CollectorMetrics {
	src := "trace_pipe"
	if len(source) > 0 && source[0] != "" {
		src = source[0]
	}
	m := &CollectorMetrics{
		up: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "netmon_loss_collector_up",
			Help: "1 if the loss collector is running and consuming events, 0 otherwise",
		}),
		eventsRead: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "netmon_loss_events_read_total",
			Help: "Total number of raw events read from the trace pipe",
		}),
		eventsParsed: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "netmon_loss_events_parsed_total",
			Help: "Total number of successfully parsed retransmit events",
		}),
		parseErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "netmon_loss_parse_errors_total",
			Help: "Total number of events that failed to parse",
		}),
		sourceInfo: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "netmon_loss_source_info",
			Help: "Information about the loss data source (labelled by source; value always 1)",
			ConstLabels: map[string]string{
				"source": src,
			},
		}),
	}

	// Register metrics
	reg.MustRegister(m.up, m.eventsRead, m.eventsParsed, m.parseErrors, m.sourceInfo)

	// Set source info to 1
	m.sourceInfo.Set(1)

	logger.Info("Collector self-metrics registered",
		zap.String("source", src))

	return m
}

// SetUp sets the collector up/down status
func (m *CollectorMetrics) SetUp(up bool) {
	if up {
		m.up.Set(1)
	} else {
		m.up.Set(0)
	}
}

// IncEventsRead increments the events read counter
func (m *CollectorMetrics) IncEventsRead() {
	m.eventsRead.Inc()
}

// IncEventsParsed increments the successfully parsed events counter
func (m *CollectorMetrics) IncEventsParsed() {
	m.eventsParsed.Inc()
}

// IncParseErrors increments the parse errors counter
func (m *CollectorMetrics) IncParseErrors() {
	m.parseErrors.Inc()
}

// SetSourceInfo sets the source info gauge
func (m *CollectorMetrics) SetSourceInfo() {
	m.sourceInfo.Set(1)
}
