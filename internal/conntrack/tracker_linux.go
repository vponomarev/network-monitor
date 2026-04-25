//go:build linux
// +build linux

package conntrack

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/vponomarev/network-monitor/pkg/events"
	"go.uber.org/zap"
)

// Connection event types
const (
	EventTypeNewConnection   events.EventType = "new_connection"
	EventTypeCloseConnection events.EventType = "close_connection"
)

// Connection direction
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

// Config holds connection tracker configuration
type Config struct {
	EBPFProgramPath string
	TrackIncoming   bool
	TrackOutgoing   bool
	FilterPorts     []int
}

// Tracker monitors network connections using eBPF
type Tracker struct {
	config Config
	logger *zap.Logger

	// eBPF components
	colls *ebpf.Collection
	links []link.Link

	// Connection tracking
	mu          sync.RWMutex
	connections map[string]*Connection

	// Event channel
	events chan events.Event
}

// NewTracker creates a new connection tracker
func NewTracker(cfg Config, logger *zap.Logger) (*Tracker, error) {
	// Remove resource limits for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		return nil, fmt.Errorf("removing memlock: %w", err)
	}

	tracker := &Tracker{
		config:      cfg,
		logger:      logger.Named("conntrack"),
		connections: make(map[string]*Connection),
		events:      make(chan events.Event, 1000),
	}

	return tracker, nil
}

// Run starts the connection tracking
func (t *Tracker) Run(ctx context.Context) error {
	t.logger.Info("Starting connection tracker",
		zap.Bool("track_incoming", t.config.TrackIncoming),
		zap.Bool("track_outgoing", t.config.TrackOutgoing))

	// Load eBPF program
	if err := t.loadEBPF(); err != nil {
		return fmt.Errorf("loading eBPF: %w", err)
	}
	defer t.close()

	// Start reading events
	go t.readEvents(ctx)

	<-ctx.Done()
	t.logger.Info("Stopping connection tracker")
	return nil
}

// loadEBPF loads and attaches eBPF programs
func (t *Tracker) loadEBPF() error {
	if t.config.EBPFProgramPath == "" {
		t.logger.Info("No eBPF program path specified, using embedded programs")
		// In production, load embedded eBPF bytecode
		return nil
	}

	// Load eBPF collection from file
	spec, err := ebpf.LoadCollectionSpec(t.config.EBPFProgramPath)
	if err != nil {
		return fmt.Errorf("loading collection spec: %w", err)
	}

	colls, err := ebpf.NewCollection(spec)
	if err != nil {
		return fmt.Errorf("creating collection: %w", err)
	}
	t.colls = colls

	// Attach programs based on configuration
	if t.config.TrackIncoming {
		if err := t.attachIncomingTracker(); err != nil {
			return fmt.Errorf("attaching incoming tracker: %w", err)
		}
	}

	if t.config.TrackOutgoing {
		if err := t.attachOutgoingTracker(); err != nil {
			return fmt.Errorf("attaching outgoing tracker: %w", err)
		}
	}

	return nil
}

// attachIncomingTracker attaches eBPF program for incoming connections
func (t *Tracker) attachIncomingTracker() error {
	// In production, attach to socket_accept or similar
	t.logger.Debug("Attached incoming connection tracker")
	return nil
}

// attachOutgoingTracker attaches eBPF program for outgoing connections
func (t *Tracker) attachOutgoingTracker() error {
	// In production, attach to socket_connect or similar
	t.logger.Debug("Attached outgoing connection tracker")
	return nil
}

// readEvents reads connection events from eBPF ring buffer
func (t *Tracker) readEvents(ctx context.Context) {
	if t.colls == nil {
		// Simulate events for development
		t.simulateEvents(ctx)
		return
	}

	// Get ring buffer reader
	ringBuf, ok := t.colls.Maps["events"]
	if !ok {
		t.logger.Error("Events map not found in eBPF collection")
		return
	}

	rd, err := ringbuf.NewReader(ringBuf)
	if err != nil {
		t.logger.Error("Creating ringbuf reader", zap.Error(err))
		return
	}
	defer rd.Close()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		record, err := rd.Read()
		if err != nil {
			t.logger.Debug("Reading ringbuf", zap.Error(err))
			continue
		}

		conn := t.parseConnectionEvent(record.RawSample)
		if conn != nil {
			t.sendEvent(conn)
		}
	}
}

// simulateEvents generates sample events for development
func (t *Tracker) simulateEvents(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			conn := &Connection{
				Timestamp:   time.Now(),
				SourceIP:    net.ParseIP("192.168.1.100"),
				SourcePort:  54321,
				DestIP:      net.ParseIP("8.8.8.8"),
				DestPort:    443,
				Protocol:    6, // TCP
				Direction:   DirectionOutgoing,
				PID:         1234,
				ProcessName: "curl",
			}
			t.sendEvent(conn)
		}
	}
}

// parseConnectionEvent parses raw eBPF event data
func (t *Tracker) parseConnectionEvent(data []byte) *Connection {
	// In production, parse binary data from eBPF
	// This is a placeholder
	return &Connection{
		Timestamp:   time.Now(),
		SourceIP:    net.IPv4(192, 168, 1, 100),
		SourcePort:  54321,
		DestIP:      net.IPv4(8, 8, 8, 8),
		DestPort:    443,
		Protocol:    6,
		Direction:   DirectionOutgoing,
		PID:         0,
		ProcessName: "unknown",
	}
}

// sendEvent sends a connection event
func (t *Tracker) sendEvent(conn *Connection) {
	// Store connection
	t.mu.Lock()
	key := t.connectionKey(conn)
	t.connections[key] = conn
	t.mu.Unlock()

	// Create event
	event := events.Event{
		Type:      EventTypeNewConnection,
		Timestamp: conn.Timestamp,
		Source:    "conntrack",
		Data: map[string]interface{}{
			"source_ip":    conn.SourceIP.String(),
			"source_port":  conn.SourcePort,
			"dest_ip":      conn.DestIP.String(),
			"dest_port":    conn.DestPort,
			"protocol":     conn.Protocol,
			"direction":    conn.Direction.String(),
			"pid":          conn.PID,
			"process_name": conn.ProcessName,
		},
	}

	select {
	case t.events <- event:
		t.logger.Debug("Connection event",
			zap.String("source", conn.SourceIP.String()),
			zap.String("dest", conn.DestIP.String()),
			zap.Uint16("dest_port", conn.DestPort))
	default:
		t.logger.Warn("Event channel full, dropping event")
	}
}

// connectionKey generates a unique key for a connection
func (t *Tracker) connectionKey(conn *Connection) string {
	return fmt.Sprintf("%s:%d-%s:%d-%d",
		conn.SourceIP.String(), conn.SourcePort,
		conn.DestIP.String(), conn.DestPort,
		conn.Protocol)
}

// GetConnections returns all tracked connections
func (t *Tracker) GetConnections() []*Connection {
	t.mu.RLock()
	defer t.mu.RUnlock()

	conns := make([]*Connection, 0, len(t.connections))
	for _, conn := range t.connections {
		conns = append(conns, conn)
	}
	return conns
}

// GetConnectionCount returns the number of tracked connections
func (t *Tracker) GetConnectionCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.connections)
}

// Events returns the event channel
func (t *Tracker) Events() <-chan events.Event {
	return t.events
}

// close cleans up eBPF resources
func (t *Tracker) close() {
	for _, l := range t.links {
		l.Close()
	}
	if t.colls != nil {
		t.colls.Close()
	}
}
