//go:build linux
// +build linux

package conntrack

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"go.uber.org/zap"
)

// Connection event types
const (
	EventTypeNewConnection   ConnectionEvent = EventNew
	EventTypeCloseConnection ConnectionEvent = EventClosed
)

// Tracker monitors network connections using eBPF
type Tracker struct {
	config Config
	logger *zap.Logger

	// eBPF components
	colls *ebpf.Collection
	links []link.Link

	// State machine
	stateMachine *StateMachine

	// Syslog writer
	syslogWriter *SyslogWriter

	// Metrics collector
	metricsCollector *MetricsCollector

	// Connection tracking
	mu          sync.RWMutex
	connections map[string]*Connection

	// Event channel
	events chan *Connection
}

// eBPF event structure (must match C struct)
type bpfConnectionEvent struct {
	TimestampNs uint64
	PidTgid     uint64
	PID         uint32
	TID         uint32
	SrcIP       [16]byte
	DstIP       [16]byte
	SrcPort     uint16
	DstPort     uint16
	Protocol    uint8
	Direction   uint8
	State       uint8
	EventType   uint8
	TCPFlags    uint8
	Comm        [16]byte
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
		events:      make(chan *Connection, 1000),
	}

	// Create metrics collector
	tracker.metricsCollector = NewMetricsCollector(logger)

	// Create state machine
	tracker.stateMachine = NewStateMachine(StateMachineConfig{
		SYNTimeout: cfg.SYNTimeout,
		OnStateChange: func(conn *Connection, oldState, newState ConnectionState) {
			tracker.logger.Debug("Connection state change",
				zap.String("conn", conn.ID),
				zap.String("old_state", oldState.String()),
				zap.String("new_state", newState.String()),
			)
		},
		OnEvent: func(conn *Connection, event ConnectionEvent) {
			tracker.onConnectionEvent(conn, event)
		},
	})

	// Create syslog writer if configured
	if cfg.Syslog.Tag != "" {
		writer, err := NewSyslogWriter(cfg.Syslog)
		if err != nil {
			logger.Warn("Failed to create syslog writer", zap.Error(err))
		} else {
			tracker.syslogWriter = writer
		}
	}

	return tracker, nil
}

// Run starts the connection tracking
func (t *Tracker) Run(ctx context.Context) error {
	t.logger.Info("Starting connection tracker",
		zap.Bool("track_incoming", t.config.TrackIncoming),
		zap.Bool("track_outgoing", t.config.TrackOutgoing),
		zap.Bool("track_closes", t.config.TrackCloses),
	)

	// Load eBPF program
	if err := t.loadEBPF(); err != nil {
		return fmt.Errorf("loading eBPF: %w", err)
	}
	defer t.close()

	// Start reading events
	go t.readEvents(ctx)

	// Start metrics update loop
	go t.updateMetrics(ctx)

	<-ctx.Done()
	t.logger.Info("Stopping connection tracker")
	return nil
}

// loadEBPF loads and attaches eBPF programs
func (t *Tracker) loadEBPF() error {
	if t.config.EBPFProgramPath == "" {
		t.logger.Info("No eBPF program path specified, using simulated events for development")
		go t.simulateEvents()
		return nil
	}

	t.logger.Info("Loading eBPF program", zap.String("path", t.config.EBPFProgramPath))

	// Load eBPF collection spec from ELF file
	spec, err := ebpf.LoadCollectionSpec(t.config.EBPFProgramPath)
	if err != nil {
		return fmt.Errorf("loading collection spec from %s: %w", t.config.EBPFProgramPath, err)
	}

	// Create collection
	colls, err := ebpf.NewCollection(spec)
	if err != nil {
		return fmt.Errorf("creating eBPF collection: %w", err)
	}
	t.colls = colls

	t.logger.Info("eBPF collection loaded successfully",
		zap.Bool("track_incoming", t.config.TrackIncoming),
		zap.Bool("track_outgoing", t.config.TrackOutgoing),
		zap.Bool("track_closes", t.config.TrackCloses))

	// Attach programs based on configuration
	if err := t.attachPrograms(); err != nil {
		return fmt.Errorf("attaching programs: %w", err)
	}

	return nil
}

// attachPrograms attaches eBPF programs to kernel hooks
func (t *Tracker) attachPrograms() error {
	// Attach tcp_connect for outgoing connections
	if t.config.TrackOutgoing {
		if prog, ok := t.colls.Programs["tcp_connect"]; ok {
			// Try fentry first (kernel 5.5+ with BTF), fallback to kprobe
			l, err := link.AttachTracing(link.TracingOptions{
				Program: prog,
			})
			if err != nil {
				t.logger.Debug("fentry tcp_connect failed, trying kprobe", zap.Error(err))
				l, err = link.Kprobe("tcp_connect", prog, nil)
				if err != nil {
					return fmt.Errorf("linking tcp_connect: %w", err)
				}
			}
			t.links = append(t.links, l)
			t.logger.Debug("Attached tcp_connect (fentry/kprobe)")
		}
	}

	// Attach tcp_v4_rcv for incoming SYN detection
	// NOTE: Disabled - IP address reading from sk_buff is unreliable
	// inet_sock_set_state provides incoming connection tracking
	if false && t.config.TrackIncoming {
		if prog, ok := t.colls.Programs["tcp_v4_rcv"]; ok {
			l, err := link.AttachTracing(link.TracingOptions{
				Program: prog,
			})
			if err != nil {
				t.logger.Warn("fentry tcp_v4_rcv failed, skipping incoming SYN detection", zap.Error(err))
			} else {
				t.links = append(t.links, l)
				t.logger.Info("Attached tcp_v4_rcv (fentry) for incoming SYN detection")
			}
		}
	}

	// Attach tcp_v4_accept for incoming connection acceptance (if available)
	if t.config.TrackIncoming {
		if prog, ok := t.colls.Programs["tcp_v4_accept"]; ok {
			// Try fentry first, fallback to kprobe
			l, err := link.AttachTracing(link.TracingOptions{
				Program: prog,
			})
			if err != nil {
				t.logger.Debug("fentry/tcp_v4_accept failed, trying kprobe", zap.Error(err))
				l, err = link.Kprobe("tcp_v4_accept", prog, nil)
				if err != nil {
					t.logger.Warn("tcp_v4_accept not available, skipping", zap.Error(err))
				} else {
					t.links = append(t.links, l)
					t.logger.Debug("Attached tcp_v4_accept (kprobe)")
				}
			} else {
				t.links = append(t.links, l)
				t.logger.Debug("Attached tcp_v4_accept (fentry)")
			}
		}
	}

	// Attach tcp_close for connection closing
	if t.config.TrackCloses {
		if prog, ok := t.colls.Programs["tcp_close"]; ok {
			// Try fentry first (kernel 5.5+ with BTF), fallback to kprobe
			l, err := link.AttachTracing(link.TracingOptions{
				Program: prog,
			})
			if err != nil {
				t.logger.Debug("fentry tcp_close failed, trying kprobe", zap.Error(err))
				l, err = link.Kprobe("tcp_close", prog, nil)
				if err != nil {
					return fmt.Errorf("linking tcp_close: %w", err)
				}
			}
			t.links = append(t.links, l)
			t.logger.Debug("Attached tcp_close (fentry/kprobe)")
		}
	}

	// Attach inet_sock_set_state tracepoint
	if prog, ok := t.colls.Programs["inet_sock_set_state"]; ok {
		l, err := link.Tracepoint("sock", "inet_sock_set_state", prog, nil)
		if err != nil {
			return fmt.Errorf("linking inet_sock_set_state: %w", err)
		}
		t.links = append(t.links, l)
		t.logger.Info("Attached inet_sock_set_state tracepoint for incoming connections")
	}

	return nil
}

// readEvents reads connection events from eBPF ring buffer
func (t *Tracker) readEvents(ctx context.Context) {
	if t.colls == nil {
		// eBPF not loaded, events will come from simulation
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
			t.processConnection(conn)
		}
	}
}

// parseConnectionEvent parses raw eBPF event data
func (t *Tracker) parseConnectionEvent(data []byte) *Connection {
	// Minimum size: timestamp(8) + pid_tgid(8) + pid(4) + tid(4) + src_ip(16) + dst_ip(16) + ports(4) + protocol(1) + direction(1) + state(1) + event_type(1) + tcp_flags(1) + comm(16) = 81 bytes
	if len(data) < 81 {
		t.logger.Debug("Event data too short", zap.Int("len", len(data)))
		return nil
	}

	// Parse binary data (must match C struct layout)
	event := &bpfConnectionEvent{}
	event.TimestampNs = binary.LittleEndian.Uint64(data[0:8])
	event.PidTgid = binary.LittleEndian.Uint64(data[8:16])
	event.PID = binary.LittleEndian.Uint32(data[16:20])
	event.TID = binary.LittleEndian.Uint32(data[20:24])
	copy(event.SrcIP[:], data[24:40])
	copy(event.DstIP[:], data[40:56])
	event.SrcPort = binary.LittleEndian.Uint16(data[56:58])
	event.DstPort = binary.LittleEndian.Uint16(data[58:60])
	event.Protocol = data[60]
	event.Direction = data[61]
	event.State = data[62]
	event.EventType = data[63]
	event.TCPFlags = data[64]
	copy(event.Comm[:], data[65:81])

	// Convert to Connection
	conn := &Connection{
		Timestamp:   time.Unix(0, int64(event.TimestampNs)),
		SourceIP:    IPFromBytes(event.SrcIP),
		SourcePort:  event.SrcPort,
		DestIP:      IPFromBytes(event.DstIP),
		DestPort:    event.DstPort,
		Protocol:    event.Protocol,
		Direction:   Direction(event.Direction),
		State:       ConnectionState(event.State),
		PID:         event.PID,
		ProcessName: sanitizeProcessName(string(event.Comm[:])),
	}

	// Generate connection ID
	conn.ID = makeConnectionKey(
		conn.SourceIP, conn.SourcePort,
		conn.DestIP, conn.DestPort,
		conn.Protocol,
	)

	return conn
}

// sanitizeProcessName cleans up process name from eBPF
func sanitizeProcessName(name string) string {
	// Remove null bytes and trim whitespace
	name = strings.TrimRight(name, "\x00")
	name = strings.TrimSpace(name)
	if name == "" {
		return "unknown"
	}
	return name
}

// processConnection processes a connection event through state machine
func (t *Tracker) processConnection(conn *Connection) {
	// Determine event type from state
	eventType := EventNew
	if conn.State == StateEstablished {
		eventType = EventEstablished
	} else if conn.State == StateClosed {
		eventType = EventClosed
	}

	// Create raw event for state machine
	evt := &ConnectionEventRaw{
		SourceIP:    conn.SourceIP,
		SourcePort:  conn.SourcePort,
		DestIP:      conn.DestIP,
		DestPort:    conn.DestPort,
		Protocol:    conn.Protocol,
		Direction:   conn.Direction,
		EventType:   eventType,
		State:       conn.State,
		PID:         conn.PID,
		ProcessName: conn.ProcessName,
		Timestamp:   conn.Timestamp,
	}

	// Process through state machine
	t.stateMachine.ProcessEvent(evt)
}

// onConnectionEvent handles connection events from state machine
func (t *Tracker) onConnectionEvent(conn *Connection, event ConnectionEvent) {
	t.logger.Debug("Connection event",
		zap.String("event", event.String()),
		zap.String("source", conn.SourceIP.String()),
		zap.String("dest", conn.DestIP.String()),
		zap.Uint16("src_port", conn.SourcePort),
		zap.Uint16("dst_port", conn.DestPort),
		zap.String("direction", conn.Direction.String()),
		zap.String("state", conn.State.String()),
	)

	// Update metrics
	if t.metricsCollector != nil {
		t.metricsCollector.OnConnectionEvent(conn, event)
	}

	// Write to syslog
	if t.syslogWriter != nil {
		if err := t.syslogWriter.WriteConnection(conn, event); err != nil {
			t.logger.Warn("Failed to write to syslog", zap.Error(err))
		}
	}

	// Store connection
	t.mu.Lock()
	t.connections[conn.ID] = conn
	t.mu.Unlock()

	// Send to event channel
	select {
	case t.events <- conn:
	default:
		t.logger.Warn("Event channel full, dropping event")
	}
}

// simulateEvents generates sample events for development
func (t *Tracker) simulateEvents() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	counter := 0
	for {
		counter++

		// Simulate outgoing connection
		outConn := &Connection{
			ID:            fmt.Sprintf("out-%d", counter),
			Timestamp:     time.Now(),
			SourceIP:      net.ParseIP("192.168.1.100"),
			SourcePort:    uint16(50000 + counter),
			DestIP:        net.ParseIP("8.8.8.8"),
			DestPort:      443,
			Protocol:      6, // TCP
			Direction:     DirectionOutgoing,
			State:         StateSynSent,
			PID:           1234,
			ProcessName:   "curl",
			SynSentTime:   time.Now(),
		}

		t.onConnectionEvent(outConn, EventNew)

		// Simulate SYN+ACK after delay
		time.Sleep(50 * time.Millisecond)
		outConn.State = StateEstablished
		outConn.Established = true
		outConn.EstablishedTime = time.Now()
		t.onConnectionEvent(outConn, EventEstablished)

		// Simulate incoming connection
		inConn := &Connection{
			ID:            fmt.Sprintf("in-%d", counter),
			Timestamp:     time.Now(),
			SourceIP:      net.ParseIP("10.0.0.50"),
			SourcePort:    uint16(40000 + counter),
			DestIP:        net.ParseIP("192.168.1.100"),
			DestPort:      80,
			Protocol:      6, // TCP
			Direction:     DirectionIncoming,
			State:         StateSynReceived,
			PID:           5678,
			ProcessName:   "nginx",
			SynSentTime:   time.Now(),
		}

		t.onConnectionEvent(inConn, EventNew)

		// Simulate accept after delay
		time.Sleep(30 * time.Millisecond)
		inConn.State = StateEstablished
		inConn.Accepted = true
		inConn.EstablishedTime = time.Now()
		t.onConnectionEvent(inConn, EventEstablished)
	}
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

// GetStats returns connection statistics
func (t *Tracker) GetStats() Stats {
	return t.stateMachine.GetStats()
}

// Events returns the event channel
func (t *Tracker) Events() <-chan *Connection {
	return t.events
}

// updateMetrics periodically updates connection state metrics
func (t *Tracker) updateMetrics(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if t.metricsCollector != nil {
				t.metricsCollector.UpdateStateMetrics(t.GetStats())
			}
		}
	}
}

// close cleans up eBPF resources
func (t *Tracker) close() {
	// Stop state machine and background goroutines
	if t.stateMachine != nil {
		t.stateMachine.Stop()
	}

	// Stop metrics collector
	if t.metricsCollector != nil {
		t.metricsCollector.Stop()
	}

	// Close eBPF links
	for _, l := range t.links {
		l.Close()
	}

	// Close eBPF collection
	if t.colls != nil {
		t.colls.Close()
	}

	// Close syslog writer
	if t.syslogWriter != nil {
		t.syslogWriter.Close()
	}
}
