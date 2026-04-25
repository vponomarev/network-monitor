package latency

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vponomarev/network-monitor/internal/config"
	"go.uber.org/zap"
)

func TestNewMonitor(t *testing.T) {
	logger := zap.NewNop()
	cfg := config.LatencyConfig{
		Targets:  []string{"8.8.8.8", "1.1.1.1"},
		Interval: 10 * time.Second,
		Timeout:  5 * time.Second,
	}

	monitor := NewMonitor(cfg, logger)

	require.NotNil(t, monitor)
	assert.Equal(t, cfg, monitor.config)
	assert.NotNil(t, monitor.results)
	assert.NotNil(t, monitor.events)
}

func TestMonitor_storeResult(t *testing.T) {
	logger := zap.NewNop()
	cfg := config.LatencyConfig{
		Targets:  []string{"8.8.8.8"},
		Interval: time.Second,
		Timeout:  time.Second,
	}

	monitor := NewMonitor(cfg, logger)

	result := &Result{
		Target:    "8.8.8.8",
		RTT:       50 * time.Millisecond,
		Timestamp: time.Now(),
		Success:   true,
	}

	monitor.storeResult(result)

	stored := monitor.GetResult("8.8.8.8")
	require.NotNil(t, stored)
	assert.Equal(t, result.RTT, stored.RTT)
	assert.True(t, stored.Success)
}

func TestMonitor_GetAllResults(t *testing.T) {
	logger := zap.NewNop()
	cfg := config.LatencyConfig{
		Targets: []string{"8.8.8.8", "1.1.1.1"},
	}

	monitor := NewMonitor(cfg, logger)

	// Initially empty
	results := monitor.GetAllResults()
	assert.Empty(t, results)

	// Add results
	monitor.storeResult(&Result{Target: "8.8.8.8", Success: true})
	monitor.storeResult(&Result{Target: "1.1.1.1", Success: true})

	results = monitor.GetAllResults()
	assert.Len(t, results, 2)
}

func TestMonitor_Events(t *testing.T) {
	logger := zap.NewNop()
	cfg := config.LatencyConfig{
		Targets: []string{"8.8.8.8"},
	}

	monitor := NewMonitor(cfg, logger)

	// Result with high latency should trigger event
	result := &Result{
		Target:    "8.8.8.8",
		RTT:       600 * time.Millisecond, // Above 500ms threshold
		Timestamp: time.Now(),
		Success:   true,
	}

	monitor.storeResult(result)

	select {
	case event := <-monitor.Events():
		assert.Equal(t, EventTypeHighLatency, event.Type)
		data, ok := event.Data.(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "8.8.8.8", data["target"])
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Expected high latency event not received")
	}
}

func TestMonitor_measureUDP(t *testing.T) {
	logger := zap.NewNop()
	cfg := config.LatencyConfig{
		Timeout: 2 * time.Second,
	}

	monitor := NewMonitor(cfg, logger)

	ctx := context.Background()
	result := monitor.measureUDP(ctx, "8.8.8.8")

	// This test requires network access, so we just check structure
	require.NotNil(t, result)
	assert.Equal(t, "8.8.8.8", result.Target)
}

func TestMonitor_Run_ContextCancellation(t *testing.T) {
	logger := zap.NewNop()
	cfg := config.LatencyConfig{
		Targets:  []string{"127.0.0.1"},
		Interval: 100 * time.Millisecond,
		Timeout:  500 * time.Millisecond,
	}

	monitor := NewMonitor(cfg, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	// Run should exit on context cancellation
	err := monitor.Run(ctx)
	assert.Error(t, err)
}
