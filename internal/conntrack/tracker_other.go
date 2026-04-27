//go:build !linux
// +build !linux

package conntrack

import (
	"context"
	"fmt"
	"go.uber.org/zap"
)

// Tracker is a stub for non-Linux platforms
type Tracker struct {
	config Config
	logger *zap.Logger
}

// NewTracker creates a stub tracker on non-Linux platforms
func NewTracker(cfg Config, logger *zap.Logger) (*Tracker, error) {
	return &Tracker{
		config: cfg,
		logger: logger.Named("conntrack"),
	}, nil
}

// Run returns an error on non-Linux platforms
func (t *Tracker) Run(ctx context.Context) error {
	return fmt.Errorf("connection tracking is only available on Linux")
}

// GetConnections returns empty slice on non-Linux platforms
func (t *Tracker) GetConnections() []*Connection {
	return []*Connection{}
}

// GetConnectionCount returns 0 on non-Linux platforms
func (t *Tracker) GetConnectionCount() int {
	return 0
}

// GetStats returns empty stats on non-Linux platforms
func (t *Tracker) GetStats() Stats {
	return Stats{}
}

// Events returns nil channel on non-Linux platforms
func (t *Tracker) Events() <-chan *Connection {
	return nil
}
