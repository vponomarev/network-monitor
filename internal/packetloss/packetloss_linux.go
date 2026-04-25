//go:build linux
// +build linux

package packetloss

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/vponomarev/network-monitor/internal/config"
	"github.com/vponomarev/network-monitor/pkg/events"
	"go.uber.org/zap"
)

// TracePipePath is the default path to the kernel trace pipe
const TracePipePath = "/sys/kernel/tracing/trace_pipe"

// Packet loss event types
const (
	EventTypePacketLoss events.EventType = "packet_loss"
	EventTypeRecovery   events.EventType = "recovery"
)

// Monitor tracks packet loss on network interfaces
type Monitor struct {
	config config.PacketLossConfig
	logger *zap.Logger

	// Statistics per interface
	mu   sync.RWMutex
	stats map[string]*interfaceStats

	// Event channel for notifications
	events chan events.Event
}

type interfaceStats struct {
	totalPackets  int
	lostPackets   int
	windowPackets []bool // true = lost, false = ok
	windowIndex   int
	lastAlert     time.Time
}

// NewMonitor creates a new packet loss monitor
func NewMonitor(cfg config.PacketLossConfig, logger *zap.Logger) *Monitor {
	return &Monitor{
		config: cfg,
		logger: logger.Named("packetloss"),
		stats:  make(map[string]*interfaceStats),
		events: make(chan events.Event, 100),
	}
}

// Run starts the packet loss monitoring
func (m *Monitor) Run(ctx context.Context) error {
	m.logger.Info("Starting packet loss monitor",
		zap.Strings("interfaces", m.config.Interfaces))

	// Initialize stats for each interface
	m.mu.Lock()
	for _, iface := range m.config.Interfaces {
		m.stats[iface] = &interfaceStats{
			windowPackets: make([]bool, m.config.WindowSize),
		}
	}
	m.mu.Unlock()

	// Start trace pipe reader
	return m.readTracePipe(ctx)
}

// readTracePipe reads from the kernel trace pipe
func (m *Monitor) readTracePipe(ctx context.Context) error {
	file, err := os.Open(TracePipePath)
	if err != nil {
		return fmt.Errorf("opening trace_pipe: %w (requires root and tracefs mounted)", err)
	}
	defer file.Close()

	reader := bufio.NewReader(file)

	// Packet loss patterns in trace output
	lossPattern := regexp.MustCompile(`(\w+):.*(?:drop|loss|timeout|retransmit)`)

	for {
		select {
		case <-ctx.Done():
			m.logger.Info("Stopping packet loss monitor")
			return ctx.Err()
		default:
		}

		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				time.Sleep(100 * time.Millisecond)
				continue
			}
			m.logger.Error("Error reading trace pipe", zap.Error(err))
			time.Sleep(100 * time.Millisecond)
			continue
		}

		m.processTraceLine(line, lossPattern)
	}
}

// processTraceLine processes a single line from trace pipe
func (m *Monitor) processTraceLine(line string, pattern *regexp.Regexp) {
	if !pattern.MatchString(line) {
		return
	}

	// Extract interface name (simplified - real implementation would parse properly)
	for _, iface := range m.config.Interfaces {
		if containsInterface(line, iface) {
			m.recordPacketLoss(iface)
			break
		}
	}
}

// containsInterface checks if the trace line mentions the interface
func containsInterface(line, iface string) bool {
	// Check for interface name in various formats
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(iface) + `\b`).MatchString(line)
}

// recordPacketLoss records a packet loss event for an interface
func (m *Monitor) recordPacketLoss(iface string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	stats, ok := m.stats[iface]
	if !ok {
		return
	}

	stats.totalPackets++
	stats.lostPackets++
	stats.windowPackets[stats.windowIndex] = true
	stats.windowIndex = (stats.windowIndex + 1) % m.config.WindowSize

	// Check if threshold exceeded
	lossPercent := m.calculateLossPercent(stats)
	if lossPercent >= m.config.ThresholdPercent {
		m.checkAndSendAlert(iface, lossPercent, stats)
	}

	m.logger.Debug("Packet loss recorded",
		zap.String("interface", iface),
		zap.Float64("loss_percent", lossPercent))
}

// calculateLossPercent calculates the current loss percentage in the window
func (m *Monitor) calculateLossPercent(stats *interfaceStats) float64 {
	if stats.totalPackets == 0 {
		return 0
	}

	// Count losses in current window
	losses := 0
	for _, lost := range stats.windowPackets {
		if lost {
			losses++
		}
	}

	return float64(losses) / float64(len(stats.windowPackets)) * 100
}

// checkAndSendAlert sends an alert if the interval has passed
func (m *Monitor) checkAndSendAlert(iface string, lossPercent float64, stats *interfaceStats) {
	now := time.Now()
	if now.Sub(stats.lastAlert) < m.config.AlertInterval {
		return
	}

	stats.lastAlert = now

	event := events.Event{
		Type:       EventTypePacketLoss,
		Timestamp:  now,
		Source:     "packetloss",
		Data: map[string]interface{}{
			"interface":    iface,
			"loss_percent": lossPercent,
			"threshold":    m.config.ThresholdPercent,
		},
	}

	select {
	case m.events <- event:
		m.logger.Warn("Packet loss threshold exceeded",
			zap.String("interface", iface),
			zap.Float64("loss_percent", lossPercent))
	default:
		m.logger.Warn("Event channel full, dropping alert")
	}
}

// GetStats returns current statistics for an interface
func (m *Monitor) GetStats(iface string) (total, lost int, lossPercent float64) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats, ok := m.stats[iface]
	if !ok {
		return 0, 0, 0
	}

	return stats.totalPackets, stats.lostPackets, m.calculateLossPercent(stats)
}

// Events returns the event channel for receiving notifications
func (m *Monitor) Events() <-chan events.Event {
	return m.events
}

// parsePacketCount extracts packet count from trace line (helper function)
func parsePacketCount(line string) (int, error) {
	// Look for patterns like "packets: 1234" or "pkt=5678"
	re := regexp.MustCompile(`(?:packets[:\s=]+|pkt[=:])(\d+)`)
	matches := re.FindStringSubmatch(line)
	if len(matches) < 2 {
		return 0, fmt.Errorf("no packet count found")
	}
	return strconv.Atoi(matches[1])
}
