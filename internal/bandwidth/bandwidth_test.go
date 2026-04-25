package bandwidth

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
	cfg := config.BandwidthConfig{
		Interfaces: []string{"eth0", "lo"},
		Interval:   5 * time.Second,
	}

	monitor := NewMonitor(cfg, logger)

	require.NotNil(t, monitor)
	assert.Equal(t, cfg, monitor.config)
	assert.NotNil(t, monitor.stats)
	assert.NotNil(t, monitor.prev)
}

func TestMonitor_GetStats_Initial(t *testing.T) {
	logger := zap.NewNop()
	cfg := config.BandwidthConfig{
		Interfaces: []string{"eth0"},
	}

	monitor := NewMonitor(cfg, logger)

	stats := monitor.GetStats("eth0")
	assert.Nil(t, stats) // No data collected yet
}

func TestMonitor_GetAllStats_Initial(t *testing.T) {
	logger := zap.NewNop()
	cfg := config.BandwidthConfig{
		Interfaces: []string{"eth0"},
	}

	monitor := NewMonitor(cfg, logger)

	stats := monitor.GetAllStats()
	assert.Empty(t, stats)
}

func TestMonitor_readProcNetDev(t *testing.T) {
	logger := zap.NewNop()
	cfg := config.BandwidthConfig{
		Interfaces: []string{"lo"},
	}

	monitor := NewMonitor(cfg, logger)

	stats, err := monitor.readProcNetDev()
	require.NoError(t, err)
	require.NotEmpty(t, stats)

	// Should have at least loopback
	lo, ok := stats["lo"]
	if ok {
		assert.NotNil(t, lo)
	}
}

func TestMonitor_Events(t *testing.T) {
	logger := zap.NewNop()
	cfg := config.BandwidthConfig{
		Interfaces: []string{"eth0"},
	}

	monitor := NewMonitor(cfg, logger)

	events := monitor.Events()
	require.NotNil(t, events)
}

func TestMonitor_Run_ContextCancellation(t *testing.T) {
	logger := zap.NewNop()
	cfg := config.BandwidthConfig{
		Interfaces: []string{"lo"},
		Interval:   100 * time.Millisecond,
	}

	monitor := NewMonitor(cfg, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	err := monitor.Run(ctx)
	assert.Error(t, err) // Context cancelled
}

func TestInterfaceStats(t *testing.T) {
	stats := &InterfaceStats{
		RxBytes:       1000,
		RxPackets:     10,
		RxErrors:      0,
		RxDropped:     0,
		TxBytes:       500,
		TxPackets:     5,
		TxErrors:      0,
		TxDropped:     0,
		RxBytesPerSec: 100.0,
		TxBytesPerSec: 50.0,
	}

	assert.Equal(t, uint64(1000), stats.RxBytes)
	assert.Equal(t, uint64(500), stats.TxBytes)
	assert.Equal(t, 100.0, stats.RxBytesPerSec)
	assert.Equal(t, 50.0, stats.TxBytesPerSec)
}
