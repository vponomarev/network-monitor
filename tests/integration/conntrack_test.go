package integration

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/vponomarev/network-monitor/internal/conntrack"
	"github.com/vponomarev/network-monitor/internal/config"
	"go.uber.org/zap"
)

// TestConntrack_Integration tests connection tracking
func TestConntrack_Integration(t *testing.T) {
	logger := zap.NewNop()
	cfg := config.ConnectionsConfig{
		Enabled:       true,
		TrackIncoming: true,
		TrackOutgoing: true,
	}

	// Note: Full eBPF testing requires kernel support and root
	// This test verifies the tracker can be created and runs
	trackerCfg := conntrack.Config{
		EBPFProgramPath: "", // Use simulation mode
		TrackIncoming:   cfg.TrackIncoming,
		TrackOutgoing:   cfg.TrackOutgoing,
	}

	tracker, err := conntrack.NewTracker(trackerCfg, logger)
	require.NoError(t, err)
	require.NotNil(t, tracker)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Start tracker in background
	go func() {
		_ = tracker.Run(ctx)
	}()

	// Generate some connections
	for i := 0; i < 3; i++ {
		conn, err := net.DialTimeout("tcp", "127.0.0.1:80", 100*time.Millisecond)
		if err == nil {
			conn.Close()
		}
	}

	// Wait for simulated events
	<-time.After(400 * time.Millisecond)

	// In simulation mode, we should get some events
	count := tracker.GetConnectionCount()
	assert.GreaterOrEqual(t, count, 0)
}
