package dns

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
	cfg := config.DNSConfig{
		Interfaces: []string{"eth0"},
		Port:       53,
		Interval:   "10s",
	}

	monitor := NewMonitor(cfg, logger)

	require.NotNil(t, monitor)
	assert.Equal(t, cfg, monitor.config)
	assert.NotNil(t, monitor.results)
	assert.NotNil(t, monitor.events)
}

func TestMonitor_storeResult(t *testing.T) {
	logger := zap.NewNop()
	cfg := config.DNSConfig{
		Interval: "1s",
	}

	monitor := NewMonitor(cfg, logger)

	result := &QueryResult{
		Domain:    "google.com",
		Server:    "8.8.8.8",
		Success:   true,
		Latency:   50 * time.Millisecond,
		Timestamp: time.Now(),
	}

	monitor.storeResult(result)

	stored := monitor.GetResult("google.com")
	require.NotNil(t, stored)
	assert.True(t, stored.Success)
	assert.Equal(t, result.Latency, stored.Latency)
}

func TestMonitor_GetAllResults(t *testing.T) {
	logger := zap.NewNop()
	cfg := config.DNSConfig{}

	monitor := NewMonitor(cfg, logger)

	// Initially empty
	results := monitor.GetAllResults()
	assert.Empty(t, results)

	// Add results
	monitor.storeResult(&QueryResult{Domain: "google.com", Success: true})
	monitor.storeResult(&QueryResult{Domain: "github.com", Success: true})

	results = monitor.GetAllResults()
	assert.Len(t, results, 2)
}

func TestMonitor_query(t *testing.T) {
	logger := zap.NewNop()
	cfg := config.DNSConfig{}

	monitor := NewMonitor(cfg, logger)

	ctx := context.Background()
	result := monitor.query(ctx, "localhost")

	// localhost should resolve
	require.NotNil(t, result)
	assert.Equal(t, "localhost", result.Domain)
}

func TestMonitor_Events_Failure(t *testing.T) {
	logger := zap.NewNop()
	cfg := config.DNSConfig{}

	monitor := NewMonitor(cfg, logger)

	// Simulate failure
	result := &QueryResult{
		Domain:    "invalid.domain.that.does.not.exist",
		Success:   false,
		Error:     assert.AnError,
		Timestamp: time.Now(),
	}

	monitor.storeResult(result)

	select {
	case event := <-monitor.Events():
		assert.Equal(t, EventTypeDNSFailure, event.Type)
		data, ok := event.Data.(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "invalid.domain.that.does.not.exist", data["domain"])
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Expected failure event not received")
	}
}

func TestMonitor_Events_Slow(t *testing.T) {
	logger := zap.NewNop()
	cfg := config.DNSConfig{}

	monitor := NewMonitor(cfg, logger)

	// Simulate slow response
	result := &QueryResult{
		Domain:    "slow.domain",
		Success:   true,
		Latency:   600 * time.Millisecond, // Above 500ms threshold
		Timestamp: time.Now(),
	}

	monitor.storeResult(result)

	select {
	case event := <-monitor.Events():
		assert.Equal(t, EventTypeDNSSlow, event.Type)
		data, ok := event.Data.(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "slow.domain", data["domain"])
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Expected slow event not received")
	}
}

func TestMonitor_Run_ContextCancellation(t *testing.T) {
	logger := zap.NewNop()
	cfg := config.DNSConfig{
		Interval: "100ms",
	}

	monitor := NewMonitor(cfg, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	err := monitor.Run(ctx)
	assert.Error(t, err) // Context cancelled
}
