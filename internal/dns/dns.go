package dns

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/vponomarev/network-monitor/internal/config"
	"github.com/vponomarev/network-monitor/pkg/events"
	"go.uber.org/zap"
)

// Event types
const (
	EventTypeDNSFailure events.EventType = "dns_failure"
	EventTypeDNSSlow    events.EventType = "dns_slow"
)

// QueryResult represents a DNS query result
type QueryResult struct {
	Domain    string
	Server    string
	Success   bool
	Records   []net.IP
	Latency   time.Duration
	Timestamp time.Time
	Error     error
}

// Monitor tracks DNS query performance
type Monitor struct {
	config config.DNSConfig
	logger *zap.Logger

	mu      sync.RWMutex
	results map[string]*QueryResult

	events chan events.Event
}

// NewMonitor creates a new DNS monitor
func NewMonitor(cfg config.DNSConfig, logger *zap.Logger) *Monitor {
	return &Monitor{
		config:  cfg,
		logger:  logger.Named("dns"),
		results: make(map[string]*QueryResult),
		events:  make(chan events.Event, 100),
	}
}

// Run starts the DNS monitoring
func (m *Monitor) Run(ctx context.Context) error {
	m.logger.Info("Starting DNS monitor",
		zap.Strings("interfaces", m.config.Interfaces),
		zap.Int("port", m.config.Port))

	ticker := time.NewTicker(m.config.Interval)
	defer ticker.Stop()

	// Test domains to monitor
	testDomains := []string{
		"google.com",
		"github.com",
		"cloudflare.com",
	}

	// Initial measurement
	m.queryAll(ctx, testDomains)

	for {
		select {
		case <-ctx.Done():
			m.logger.Info("Stopping DNS monitor")
			return ctx.Err()
		case <-ticker.C:
			m.queryAll(ctx, testDomains)
		}
	}
}

// queryAll queries all test domains
func (m *Monitor) queryAll(ctx context.Context, domains []string) {
	var wg sync.WaitGroup

	for _, domain := range domains {
		wg.Add(1)
		go func(d string) {
			defer wg.Done()
			result := m.query(ctx, d)
			m.storeResult(result)
		}(domain)
	}

	wg.Wait()
}

// query performs a DNS query for a domain
func (m *Monitor) query(ctx context.Context, domain string) *QueryResult {
	start := time.Now()

	// Use system resolver
	ips, err := net.LookupIP(domain)
	latency := time.Since(start)

	result := &QueryResult{
		Domain:    domain,
		Server:    "system",
		Success:   err == nil,
		Records:   ips,
		Latency:   latency,
		Timestamp: start,
		Error:     err,
	}

	if err != nil {
		m.logger.Debug("DNS query failed",
			zap.String("domain", domain),
			zap.Error(err))
	}

	return result
}

// storeResult stores a query result
func (m *Monitor) storeResult(result *QueryResult) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.results[result.Domain] = result

	if !result.Success {
		m.sendFailureAlert(result)
	} else if result.Latency > 500*time.Millisecond {
		m.sendSlowAlert(result)
	}
}

// sendFailureAlert sends an alert for DNS failure
func (m *Monitor) sendFailureAlert(result *QueryResult) {
	event := events.Event{
		Type:      EventTypeDNSFailure,
		Timestamp: result.Timestamp,
		Source:    "dns",
		Data: map[string]interface{}{
			"domain": result.Domain,
			"error":  result.Error.Error(),
		},
	}

	select {
	case m.events <- event:
		m.logger.Warn("DNS failure detected",
			zap.String("domain", result.Domain),
			zap.Error(result.Error))
	default:
		m.logger.Warn("Event channel full, dropping alert")
	}
}

// sendSlowAlert sends an alert for slow DNS response
func (m *Monitor) sendSlowAlert(result *QueryResult) {
	event := events.Event{
		Type:      EventTypeDNSSlow,
		Timestamp: result.Timestamp,
		Source:    "dns",
		Data: map[string]interface{}{
			"domain":    result.Domain,
			"latency_ms": result.Latency.Milliseconds(),
		},
	}

	select {
	case m.events <- event:
		m.logger.Warn("Slow DNS response detected",
			zap.String("domain", result.Domain),
			zap.Duration("latency", result.Latency))
	default:
		m.logger.Warn("Event channel full, dropping alert")
	}
}

// GetResult returns the latest result for a domain
func (m *Monitor) GetResult(domain string) *QueryResult {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.results[domain]
}

// GetAllResults returns all results
func (m *Monitor) GetAllResults() map[string]*QueryResult {
	m.mu.RLock()
	defer m.mu.RUnlock()

	results := make(map[string]*QueryResult)
	for k, v := range m.results {
		results[k] = v
	}
	return results
}

// Events returns the event channel
func (m *Monitor) Events() <-chan events.Event {
	return m.events
}
