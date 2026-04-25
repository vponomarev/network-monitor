//go:build !linux
// +build !linux

package conntrack

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/vponomarev/network-monitor/pkg/events"
	"go.uber.org/zap"
)

// Config holds connection tracker configuration
type Config struct {
	EBPFProgramPath string
	TrackIncoming   bool
	TrackOutgoing   bool
	FilterPorts     []int
}

// Tracker is a stub for non-Linux platforms
type Tracker struct {
	config      Config
	logger      *zap.Logger
	connections map[string]*Connection
	events      chan events.Event
}

// Connection represents a network connection
type Connection struct {
	Timestamp   time.Time
	SourceIP    net.IP
	SourcePort  uint16
	DestIP      net.IP
	DestPort    uint16
	Protocol    uint8
	Direction   Direction
	PID         uint32
	ProcessName string
}

// Direction represents connection direction
type Direction int

const (
	DirectionIncoming Direction = iota
	DirectionOutgoing
)

func (d Direction) String() string {
	switch d {
	case DirectionIncoming:
		return "incoming"
	case DirectionOutgoing:
		return "outgoing"
	default:
		return "unknown"
	}
}

// NewTracker creates a stub tracker on non-Linux platforms
func NewTracker(cfg Config, logger *zap.Logger) (*Tracker, error) {
	return &Tracker{
		config:      cfg,
		logger:      logger.Named("conntrack"),
		connections: make(map[string]*Connection),
		events:      make(chan events.Event, 1000),
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

// Events returns the event channel
func (t *Tracker) Events() <-chan events.Event {
	return t.events
}
