//go:build !linux
// +build !linux

package latency

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/vponomarev/network-monitor/internal/config"
	"github.com/vponomarev/network-monitor/pkg/events"
	"go.uber.org/zap"
)

// Event types
const (
	EventTypeHighLatency events.EventType = "high_latency"
	EventTypeTimeout     events.EventType = "timeout"
)

// Result represents a latency measurement result
type Result struct {
	Target    string
	RTT       time.Duration
	Timestamp time.Time
	Success   bool
	Error     error
}

// Monitor tracks network latency to targets
type Monitor struct {
	config config.LatencyConfig
	logger *zap.Logger

	mu      sync.RWMutex
	results map[string]*Result

	events chan events.Event
}

// NewMonitor creates a new latency monitor
func NewMonitor(cfg config.LatencyConfig, logger *zap.Logger) *Monitor {
	return &Monitor{
		config:  cfg,
		logger:  logger.Named("latency"),
		results: make(map[string]*Result),
		events:  make(chan events.Event, 100),
	}
}

// Run starts the latency monitoring
func (m *Monitor) Run(ctx context.Context) error {
	interval := m.config.IntervalDuration()
	m.logger.Info("Starting latency monitor (UDP mode - ICMP requires Linux)",
		zap.Strings("targets", m.config.Targets),
		zap.Duration("interval", interval))

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Initial measurement
	m.measureAllUDP(ctx)

	for {
		select {
		case <-ctx.Done():
			m.logger.Info("Stopping latency monitor")
			return ctx.Err()
		case <-ticker.C:
			m.measureAllUDP(ctx)
		}
	}
}

// measureAllUDP measures latency to all targets using UDP
func (m *Monitor) measureAllUDP(ctx context.Context) {
	var wg sync.WaitGroup

	for _, target := range m.config.Targets {
		wg.Add(1)
		go func(t string) {
			defer wg.Done()
			result := m.measureUDP(ctx, t)
			m.storeResult(result)
		}(target)
	}

	wg.Wait()
}

// measureUDP measures latency using UDP
func (m *Monitor) measureUDP(ctx context.Context, target string) *Result {
	start := time.Now()
	timeout := m.config.TimeoutDuration()

	conn, err := net.DialTimeout("udp", fmt.Sprintf("%s:53", target), timeout)
	if err != nil {
		return &Result{
			Target:    target,
			Timestamp: start,
			Success:   false,
			Error:     fmt.Errorf("dialing: %w", err),
		}
	}
	defer conn.Close()

	// Simple DNS query for latency test
	dnsQuery := []byte{
		0x00, 0x01, // ID
		0x01, 0x00, // Flags: standard query
		0x00, 0x01, // Questions: 1
		0x00, 0x00, // Answer RRs: 0
		0x00, 0x00, // Authority RRs: 0
		0x00, 0x00, // Additional RRs: 0
		// Query: example.com
		0x07, 'e', 'x', 'a', 'm', 'p', 'l', 'e',
		0x03, 'c', 'o', 'm',
		0x00,       // Null terminator
		0x00, 0x01, // Type: A
		0x00, 0x01, // Class: IN
	}

	conn.SetDeadline(time.Now().Add(timeout))

	if _, err := conn.Write(dnsQuery); err != nil {
		return &Result{
			Target:    target,
			Timestamp: start,
			Success:   false,
			Error:     fmt.Errorf("writing: %w", err),
		}
	}

	reply := make([]byte, 512)
	if _, err := conn.Read(reply); err != nil {
		return &Result{
			Target:    target,
			Timestamp: start,
			Success:   false,
			Error:     fmt.Errorf("reading: %w", err),
		}
	}

	rtt := time.Since(start)

	return &Result{
		Target:    target,
		RTT:       rtt,
		Timestamp: start,
		Success:   true,
	}
}

// storeResult stores a measurement result
func (m *Monitor) storeResult(result *Result) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.results[result.Target] = result

	if !result.Success {
		m.logger.Debug("Latency measurement failed",
			zap.String("target", result.Target),
			zap.Error(result.Error))
		return
	}

	m.logger.Debug("Latency measured",
		zap.String("target", result.Target),
		zap.Duration("rtt", result.RTT))

	// Check for high latency threshold (e.g., > 500ms)
	if result.RTT > 500*time.Millisecond {
		m.sendHighLatencyAlert(result)
	}
}

// sendHighLatencyAlert sends an alert for high latency
func (m *Monitor) sendHighLatencyAlert(result *Result) {
	event := events.Event{
		Type:      EventTypeHighLatency,
		Timestamp: result.Timestamp,
		Source:    "latency",
		Data: map[string]interface{}{
			"target": result.Target,
			"rtt_ms": result.RTT.Milliseconds(),
		},
	}

	select {
	case m.events <- event:
		m.logger.Warn("High latency detected",
			zap.String("target", result.Target),
			zap.Duration("rtt", result.RTT))
	default:
		m.logger.Warn("Event channel full, dropping alert")
	}
}

// GetResult returns the latest result for a target
func (m *Monitor) GetResult(target string) *Result {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.results[target]
}

// GetAllResults returns all results
func (m *Monitor) GetAllResults() map[string]*Result {
	m.mu.RLock()
	defer m.mu.RUnlock()

	results := make(map[string]*Result)
	for k, v := range m.results {
		results[k] = v
	}
	return results
}

// Events returns the event channel
func (m *Monitor) Events() <-chan events.Event {
	return m.events
}
