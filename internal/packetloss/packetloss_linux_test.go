//go:build linux
// +build linux

package packetloss

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vponomarev/network-monitor/internal/config"
	"go.uber.org/zap"
)

func TestNewMonitor(t *testing.T) {
	logger := zap.NewNop()
	cfg := config.PacketLossConfig{
		Interfaces:       []string{"eth0", "eth1"},
		ThresholdPercent: 2.0,
		WindowSize:       50,
	}

	monitor := NewMonitor(cfg, logger)

	require.NotNil(t, monitor)
	assert.Equal(t, cfg, monitor.config)
	assert.NotNil(t, monitor.events)
}

func TestMonitor_GetStats_Initial(t *testing.T) {
	logger := zap.NewNop()
	cfg := config.PacketLossConfig{
		Interfaces: []string{"eth0"},
		WindowSize: 100,
	}

	monitor := NewMonitor(cfg, logger)

	total, lost, percent := monitor.GetStats("eth0")
	assert.Equal(t, 0, total)
	assert.Equal(t, 0, lost)
	assert.Equal(t, 0.0, percent)

	// Non-existent interface
	total, lost, percent = monitor.GetStats("eth99")
	assert.Equal(t, 0, total)
	assert.Equal(t, 0, lost)
	assert.Equal(t, 0.0, percent)
}

func TestMonitor_recordPacketLoss(t *testing.T) {
	logger := zap.NewNop()
	cfg := config.PacketLossConfig{
		Interfaces:       []string{"eth0"},
		ThresholdPercent: 50.0, // High threshold to avoid alerts
		WindowSize:       10,
		AlertInterval:    "1s",
	}

	monitor := NewMonitor(cfg, logger)

	// Record some losses
	for i := 0; i < 5; i++ {
		monitor.recordPacketLoss("eth0")
	}

	total, lost, percent := monitor.GetStats("eth0")
	assert.Equal(t, 5, total)
	assert.Equal(t, 5, lost)
	assert.Equal(t, 50.0, percent)
}

func TestMonitor_calculateLossPercent(t *testing.T) {
	logger := zap.NewNop()
	cfg := config.PacketLossConfig{
		Interfaces: []string{"eth0"},
		WindowSize: 100,
	}

	monitor := NewMonitor(cfg, logger)

	// Manually set up stats
	monitor.mu.Lock()
	stats := &interfaceStats{
		totalPackets:  100,
		lostPackets:   25,
		windowPackets: make([]bool, 100),
	}
	// Mark 25 packets as lost
	for i := 0; i < 25; i++ {
		stats.windowPackets[i] = true
	}
	monitor.stats["eth0"] = stats
	monitor.mu.Unlock()

	_, _, percent := monitor.GetStats("eth0")
	assert.Equal(t, 25.0, percent)
}

func TestMonitor_Events(t *testing.T) {
	logger := zap.NewNop()
	cfg := config.PacketLossConfig{
		Interfaces:       []string{"eth0"},
		ThresholdPercent: 10.0,
		WindowSize:       10,
		AlertInterval:    "1ms",
	}

	monitor := NewMonitor(cfg, logger)

	// Record enough losses to trigger alert
	for i := 0; i < 5; i++ {
		monitor.recordPacketLoss("eth0")
	}

	// Wait for event
	select {
	case event := <-monitor.Events():
		assert.Equal(t, EventTypePacketLoss, event.Type)
		assert.Equal(t, "packetloss", event.Source)
		data, ok := event.Data.(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "eth0", data["interface"])
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Expected event not received")
	}
}

func TestMonitor_processTraceLine(t *testing.T) {
	logger := zap.NewNop()
	cfg := config.PacketLossConfig{
		Interfaces:       []string{"eth0"},
		ThresholdPercent: 50.0,
		WindowSize:       100,
	}

	monitor := NewMonitor(cfg, logger)
	pattern := regexp.MustCompile(`(\w+):.*(?:drop|loss|timeout|retransmit)`)

	// Line with packet loss
	monitor.processTraceLine("eth0: packet drop detected", pattern)
	total, lost, _ := monitor.GetStats("eth0")
	assert.Equal(t, 1, total)
	assert.Equal(t, 1, lost)

	// Line without loss
	monitor.processTraceLine("eth0: packet transmitted successfully", pattern)
	total, lost, _ = monitor.GetStats("eth0")
	assert.Equal(t, 1, total) // Should not increase
	assert.Equal(t, 1, lost)
}

func Test_containsInterface(t *testing.T) {
	tests := []struct {
		line     string
		iface    string
		expected bool
	}{
		{"eth0: packet drop", "eth0", true},
		{"eth10: packet drop", "eth1", false},
		{"packet drop on eth0 interface", "eth0", true},
		{"no interface mentioned", "eth0", false},
	}

	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			result := containsInterface(tt.line, tt.iface)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func Test_parsePacketCount(t *testing.T) {
	tests := []struct {
		line     string
		expected int
		hasError bool
	}{
		{"packets: 1234", 1234, false},
		{"pkt=5678", 5678, false},
		{"pkt: 999", 999, false},
		{"no count here", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			count, err := parsePacketCount(tt.line)
			if tt.hasError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, count)
			}
		})
	}
}

func TestMonitor_Run_ContextCancellation(t *testing.T) {
	logger := zap.NewNop()
	cfg := config.PacketLossConfig{
		Interfaces:    []string{"lo"},
		WindowSize:    100,
		AlertInterval: "1s",
	}

	monitor := NewMonitor(cfg, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// This will fail to open trace_pipe without root, but should handle context cancellation
	err := monitor.Run(ctx)
	// Error is expected if not running as root
	if err != nil {
		assert.Contains(t, err.Error(), "requires root")
	}
}
