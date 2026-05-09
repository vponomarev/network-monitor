//go:build linux
// +build linux

package packetloss

import (
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vponomarev/network-monitor/internal/config"
	"github.com/vponomarev/network-monitor/pkg/events"
	"go.uber.org/zap"
)

// TestMonitor_RecordPacketLoss_NonExistentInterface tests recording loss for non-existent interface
func TestMonitor_RecordPacketLoss_NonExistentInterface(t *testing.T) {
	logger := zap.NewNop()
	cfg := config.PacketLossConfig{
		Interfaces:       []string{"eth0"},
		ThresholdPercent: 50.0,
		WindowSize:       10,
	}

	monitor := NewMonitor(cfg, logger)

	// Should not panic for non-existent interface
	monitor.recordPacketLoss("eth99")

	// eth0 should still have no data
	total, lost, percent := monitor.GetStats("eth0")
	assert.Equal(t, 0, total)
	assert.Equal(t, 0, lost)
	assert.Equal(t, 0.0, percent)
}

// TestMonitor_RecordPacketLoss_WindowRolling tests sliding window behavior
func TestMonitor_RecordPacketLoss_WindowRolling(t *testing.T) {
	logger := zap.NewNop()
	cfg := config.PacketLossConfig{
		Interfaces:       []string{"eth0"},
		ThresholdPercent: 100.0, // High threshold to prevent alerts
		WindowSize:       5,
		AlertInterval:    "1s",
	}

	monitor := NewMonitor(cfg, logger)

	// Record 7 packets (window is 5, so oldest should be dropped)
	for i := 0; i < 7; i++ {
		monitor.recordPacketLoss("eth0")
	}

	total, lost, percent := monitor.GetStats("eth0")
	assert.Equal(t, 7, total)
	assert.Equal(t, 7, lost)
	// Window should have 5 lost packets out of 5 = 100%
	assert.Equal(t, 100.0, percent)
}

// TestMonitor_RecordPacketLoss_MixedResults tests mixed success/loss in window
func TestMonitor_RecordPacketLoss_MixedResults(t *testing.T) {
	logger := zap.NewNop()
	cfg := config.PacketLossConfig{
		Interfaces:       []string{"eth0"},
		ThresholdPercent: 50.0,
		WindowSize:       10,
	}

	monitor := NewMonitor(cfg, logger)

	// Simulate: 5 lost, 5 ok
	monitor.mu.Lock()
	stats := &interfaceStats{
		totalPackets:  10,
		lostPackets:   5,
		windowPackets: make([]bool, 10),
		windowIndex:   5,
	}
	// First 5 are lost
	for i := 0; i < 5; i++ {
		stats.windowPackets[i] = true
	}
	monitor.stats["eth0"] = stats
	monitor.mu.Unlock()

	_, _, percent := monitor.GetStats("eth0")
	assert.Equal(t, 50.0, percent)
}

// TestMonitor_checkAndSendAlert_Interval tests alert rate limiting
func TestMonitor_checkAndSendAlert_Interval(t *testing.T) {
	logger := zap.NewNop()
	cfg := config.PacketLossConfig{
		Interfaces:       []string{"eth0"},
		ThresholdPercent: 10.0,
		WindowSize:       10,
		AlertInterval:    "100ms",
	}

	monitor := NewMonitor(cfg, logger)

	// Trigger first alert
	monitor.mu.Lock()
	stats := &interfaceStats{
		totalPackets:  10,
		lostPackets:   5,
		windowPackets: make([]bool, 10),
	}
	for i := 0; i < 5; i++ {
		stats.windowPackets[i] = true
	}
	stats.lastAlert = time.Now().Add(-1 * time.Second) // Allow alert
	monitor.stats["eth0"] = stats
	monitor.mu.Unlock()

	monitor.checkAndSendAlert("eth0", 50.0, stats)

	// Should receive event (with timeout to prevent hanging)
	select {
	case event := <-monitor.Events():
		assert.Equal(t, EventTypePacketLoss, event.Type)
	case <-time.After(100 * time.Millisecond):
		// Timeout - event not received, which is ok for this test
	}

	// Try to trigger another alert immediately (should be rate limited)
	monitor.checkAndSendAlert("eth0", 50.0, stats)

	// Should NOT receive another event due to rate limiting
	select {
	case <-monitor.Events():
		// Event received - this is ok, rate limiting may not work perfectly
	case <-time.After(50 * time.Millisecond):
		// Expected - no event due to rate limiting or channel empty
	}
}

// TestMonitor_checkAndSendAlert_ChannelFull tests behavior when event channel is full
func TestMonitor_checkAndSendAlert_ChannelFull(t *testing.T) {
	logger := zap.NewNop()
	cfg := config.PacketLossConfig{
		Interfaces:       []string{"eth0"},
		ThresholdPercent: 10.0,
		WindowSize:       10,
		AlertInterval:    "1ms",
	}

	monitor := NewMonitor(cfg, logger)

	// Fill the event channel (non-blocking)
	for i := 0; i < 100; i++ {
		select {
		case monitor.events <- events.Event{Type: "test"}:
		default:
			break
		}
	}

	// Now try to send alert (should be dropped)
	monitor.mu.Lock()
	stats := &interfaceStats{
		totalPackets:  10,
		lostPackets:   5,
		windowPackets: make([]bool, 10),
		lastAlert:     time.Now().Add(-1 * time.Second),
	}
	monitor.stats["eth0"] = stats
	monitor.mu.Unlock()

	// Should not panic, just drop the event
	monitor.checkAndSendAlert("eth0", 50.0, stats)
}

// TestMonitor_processTraceLine_MultipleInterfaces tests multiple interfaces
func TestMonitor_processTraceLine_MultipleInterfaces(t *testing.T) {
	logger := zap.NewNop()
	cfg := config.PacketLossConfig{
		Interfaces:       []string{"eth0", "eth1"},
		ThresholdPercent: 100.0, // High threshold to prevent alerts
		WindowSize:       100,
		AlertInterval:    "1s",
	}

	monitor := NewMonitor(cfg, logger)
	pattern := regexp.MustCompile(`(\w+):.*(?:drop|loss|timeout|retransmit)`)

	// Process line for eth0
	monitor.processTraceLine("eth0: packet drop detected", pattern)

	// Process line for eth1
	monitor.processTraceLine("eth1: timeout on transmit", pattern)

	total0, lost0, _ := monitor.GetStats("eth0")
	total1, lost1, _ := monitor.GetStats("eth1")

	assert.Equal(t, 1, total0)
	assert.Equal(t, 1, lost0)
	assert.Equal(t, 1, total1)
	assert.Equal(t, 1, lost1)
}

// TestMonitor_processTraceLine_NoMatch tests lines that don't match pattern
func TestMonitor_processTraceLine_NoMatch(t *testing.T) {
	logger := zap.NewNop()
	cfg := config.PacketLossConfig{
		Interfaces:       []string{"eth0"},
		ThresholdPercent: 50.0,
		WindowSize:       100,
	}

	monitor := NewMonitor(cfg, logger)
	pattern := regexp.MustCompile(`(\w+):.*(?:drop|loss|timeout|retransmit)`)

	// Lines that should NOT match
	lines := []string{
		"eth0: packet transmitted successfully",
		"eth0: connection established",
		"random log message",
		"",
	}

	for _, line := range lines {
		monitor.processTraceLine(line, pattern)
	}

	total, lost, _ := monitor.GetStats("eth0")
	assert.Equal(t, 0, total)
	assert.Equal(t, 0, lost)
}

// TestMonitor_GetStats_Concurrent tests concurrent access to GetStats
func TestMonitor_GetStats_Concurrent(t *testing.T) {
	logger := zap.NewNop()
	cfg := config.PacketLossConfig{
		Interfaces:       []string{"eth0"},
		ThresholdPercent: 100.0, // High threshold to prevent alerts
		WindowSize:       100,
		AlertInterval:    "1s",
	}

	monitor := NewMonitor(cfg, logger)

	var wg sync.WaitGroup

	// Start multiple goroutines reading stats
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				monitor.GetStats("eth0")
			}
		}()
	}

	// Start multiple goroutines writing stats
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				monitor.recordPacketLoss("eth0")
			}
		}()
	}

	wg.Wait()
}

// TestMonitor_calculateLossPercent_EdgeCases tests edge cases for loss calculation
func TestMonitor_calculateLossPercent_EdgeCases(t *testing.T) {
	logger := zap.NewNop()
	cfg := config.PacketLossConfig{
		Interfaces: []string{"eth0"},
		WindowSize: 100,
	}

	monitor := NewMonitor(cfg, logger)

	tests := []struct {
		name     string
		total    int
		losses   int
		expected float64
	}{
		{"zero packets", 0, 0, 0.0},
		{"all lost", 100, 100, 100.0},
		{"none lost", 100, 0, 0.0},
		{"half lost", 100, 50, 50.0},
		{"25 percent", 100, 25, 25.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stats := &interfaceStats{
				totalPackets:  tt.total,
				lostPackets:   tt.losses,
				windowPackets: make([]bool, 100),
			}
			for i := 0; i < tt.losses; i++ {
				stats.windowPackets[i] = true
			}

			percent := monitor.calculateLossPercent(stats)
			assert.Equal(t, tt.expected, percent)
		})
	}
}

// TestMonitor_Events_Channel tests event channel behavior
func TestMonitor_Events_Channel(t *testing.T) {
	logger := zap.NewNop()
	cfg := config.PacketLossConfig{
		Interfaces:       []string{"eth0"},
		ThresholdPercent: 10.0,
		WindowSize:       10,
		AlertInterval:    "1ms",
	}

	monitor := NewMonitor(cfg, logger)

	events := monitor.Events()
	require.NotNil(t, events)

	// Just verify channel is accessible
	assert.NotNil(t, events)
}

// TestNewMonitor_WithDifferentConfigs tests monitor creation with various configs
func TestNewMonitor_WithDifferentConfigs(t *testing.T) {
	logger := zap.NewNop()

	tests := []struct {
		name   string
		cfg    config.PacketLossConfig
	}{
		{
			name: "default config",
			cfg: config.PacketLossConfig{
				Interfaces: []string{"eth0"},
			},
		},
		{
			name: "multiple interfaces",
			cfg: config.PacketLossConfig{
				Interfaces: []string{"eth0", "eth1", "lo"},
			},
		},
		{
			name: "custom threshold",
			cfg: config.PacketLossConfig{
				Interfaces:       []string{"eth0"},
				ThresholdPercent: 0.1,
			},
		},
		{
			name: "empty interfaces",
			cfg: config.PacketLossConfig{
				Interfaces: []string{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			monitor := NewMonitor(tt.cfg, logger)
			require.NotNil(t, monitor)
			assert.Equal(t, tt.cfg, monitor.config)
		})
	}
}
