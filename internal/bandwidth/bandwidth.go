package bandwidth

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vponomarev/network-monitor/internal/config"
	"go.uber.org/zap"
)

// ProcNetDevPath is the path to network device statistics
const ProcNetDevPath = "/proc/net/dev"

// InterfaceStats holds bandwidth statistics for an interface
type InterfaceStats struct {
	RxBytes      uint64
	RxPackets    uint64
	RxErrors     uint64
	RxDropped    uint64
	TxBytes      uint64
	TxPackets    uint64
	TxErrors     uint64
	TxDropped    uint64
	Timestamp    time.Time
	RxBytesPerSec  float64
	TxBytesPerSec  float64
}

// Monitor tracks network bandwidth usage
type Monitor struct {
	config config.BandwidthConfig
	logger *zap.Logger

	mu   sync.RWMutex
	stats map[string]*InterfaceStats
	prev  map[string]*InterfaceStats

	events chan error
}

// NewMonitor creates a new bandwidth monitor
func NewMonitor(cfg config.BandwidthConfig, logger *zap.Logger) *Monitor {
	return &Monitor{
		config: cfg,
		logger: logger.Named("bandwidth"),
		stats:  make(map[string]*InterfaceStats),
		prev:   make(map[string]*InterfaceStats),
		events: make(chan error, 10),
	}
}

// Run starts the bandwidth monitoring
func (m *Monitor) Run(ctx context.Context) error {
	m.logger.Info("Starting bandwidth monitor",
		zap.Strings("interfaces", m.config.Interfaces),
		zap.Duration("interval", m.config.Interval))

	ticker := time.NewTicker(m.config.Interval)
	defer ticker.Stop()

	// Initial measurement
	m.collect()

	for {
		select {
		case <-ctx.Done():
			m.logger.Info("Stopping bandwidth monitor")
			return ctx.Err()
		case <-ticker.C:
			m.collect()
		}
	}
}

// collect collects bandwidth statistics
func (m *Monitor) collect() {
	current, err := m.readProcNetDev()
	if err != nil {
		m.logger.Error("Reading /proc/net/dev", zap.Error(err))
		select {
		case m.events <- err:
		default:
		}
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()

	for _, iface := range m.config.Interfaces {
		stats, ok := current[iface]
		if !ok {
			m.logger.Debug("Interface not found", zap.String("interface", iface))
			continue
		}

		// Calculate rates if we have previous data
		if prev, ok := m.prev[iface]; ok {
			duration := now.Sub(prev.Timestamp).Seconds()
			if duration > 0 {
				stats.RxBytesPerSec = float64(stats.RxBytes-prev.RxBytes) / duration
				stats.TxBytesPerSec = float64(stats.TxBytes-prev.TxBytes) / duration
			}
		}

		stats.Timestamp = now
		m.stats[iface] = stats
		m.prev[iface] = stats
	}

	m.logStats()
}

// readProcNetDev reads network statistics from /proc/net/dev
func (m *Monitor) readProcNetDev() (map[string]*InterfaceStats, error) {
	file, err := os.Open(ProcNetDevPath)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", ProcNetDevPath, err)
	}
	defer file.Close()

	stats := make(map[string]*InterfaceStats)
	scanner := bufio.NewScanner(file)

	// Skip header lines
	scanner.Scan() // Header line 1
	scanner.Scan() // Header line 2

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		parts := strings.Split(line, ":")
		if len(parts) != 2 {
			continue
		}

		iface := strings.TrimSpace(parts[0])
		values := strings.Fields(parts[1])

		if len(values) < 8 {
			continue
		}

		rxBytes, _ := strconv.ParseUint(values[0], 10, 64)
		rxPackets, _ := strconv.ParseUint(values[1], 10, 64)
		rxErrors, _ := strconv.ParseUint(values[2], 10, 64)
		rxDropped, _ := strconv.ParseUint(values[3], 10, 64)
		txBytes, _ := strconv.ParseUint(values[8], 10, 64)
		txPackets, _ := strconv.ParseUint(values[9], 10, 64)
		txErrors, _ := strconv.ParseUint(values[10], 10, 64)
		txDropped, _ := strconv.ParseUint(values[11], 10, 64)

		stats[iface] = &InterfaceStats{
			RxBytes:   rxBytes,
			RxPackets: rxPackets,
			RxErrors:  rxErrors,
			RxDropped: rxDropped,
			TxBytes:   txBytes,
			TxPackets: txPackets,
			TxErrors:  txErrors,
			TxDropped: txDropped,
		}
	}

	return stats, scanner.Err()
}

// logStats logs current statistics
func (m *Monitor) logStats() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for iface, stats := range m.stats {
		m.logger.Debug("Bandwidth stats",
			zap.String("interface", iface),
			zap.Float64("rx_bytes_per_sec", stats.RxBytesPerSec),
			zap.Float64("tx_bytes_per_sec", stats.TxBytesPerSec))
	}
}

// GetStats returns current statistics for an interface
func (m *Monitor) GetStats(iface string) *InterfaceStats {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.stats[iface]
}

// GetAllStats returns statistics for all monitored interfaces
func (m *Monitor) GetAllStats() map[string]*InterfaceStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]*InterfaceStats)
	for k, v := range m.stats {
		result[k] = v
	}
	return result
}

// Events returns the error event channel
func (m *Monitor) Events() <-chan error {
	return m.events
}
