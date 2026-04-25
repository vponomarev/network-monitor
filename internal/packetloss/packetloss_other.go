//go:build !linux
// +build !linux

package packetloss

import (
	"context"
	"fmt"

	"github.com/vponomarev/network-monitor/internal/config"
	"github.com/vponomarev/network-monitor/pkg/events"
	"go.uber.org/zap"
)

// TracePipePath is not available on non-Linux platforms
const TracePipePath = ""

// Monitor is a stub for non-Linux platforms
type Monitor struct {
	config config.PacketLossConfig
	logger *zap.Logger
}

// NewMonitor creates a stub monitor on non-Linux platforms
func NewMonitor(cfg config.PacketLossConfig, logger *zap.Logger) *Monitor {
	return &Monitor{
		config: cfg,
		logger: logger.Named("packetloss"),
	}
}

// Run returns an error on non-Linux platforms
func (m *Monitor) Run(ctx context.Context) error {
	return fmt.Errorf("packet loss monitoring is only available on Linux")
}

// GetStats returns zeros on non-Linux platforms
func (m *Monitor) GetStats(iface string) (int, int, float64) {
	return 0, 0, 0
}

// Events returns nil on non-Linux platforms
func (m *Monitor) Events() <-chan events.Event {
	return nil
}
