//go:build linux
// +build linux

package conntrack

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/vponomarev/network-monitor/pkg/embedded"
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
// C struct has __u8 _pad[7] after tcp_flags for 8-byte alignment of comm
type bpfConnectionEvent struct {
	TimestampNs uint64    // offset 0
	PidTgid     uint64    // offset 8
	PID         uint32    // offset 16
	TID         uint32    // offset 20
	SrcIP       [16]byte  // offset 24
	DstIP       [16]byte  // offset 40
	SrcPort     uint16    // offset 56
	DstPort     uint16    // offset 58
	Protocol    uint8     // offset 60
	Direction   uint8     // offset 61
	State       uint8     // offset 62
	EventType   uint8     // offset 63
	TCPFlags    uint8     // offset 64
	_           [7]byte   // offset 65-71 (padding for comm alignment)
	Comm        [16]byte  // offset 72
}

// validateBpfConnectionEvent checks that Go struct matches C struct
func validateBpfConnectionEvent() error {
	// C struct: 8+8+4+4+16+16+2+2+1+1+1+1+1+7(pad)+16 = 88 bytes
	if unsafe.Sizeof(bpfConnectionEvent{}) != 88 {
		return fmt.Errorf("bpfConnectionEvent size mismatch: got %d, expected 88", 
			unsafe.Sizeof(bpfConnectionEvent{}))
	}
	
	// Comm must start at offset 72 (after 7-byte padding)
	if unsafe.Offsetof(bpfConnectionEvent{}.Comm) != 72 {
		return fmt.Errorf("bpfConnectionEvent.Comm offset mismatch: got %d, expected 72", 
			unsafe.Offsetof(bpfConnectionEvent{}.Comm))
	}
	
	return nil
}

// NewTracker creates a new connection tracker
func NewTracker(cfg Config, logger *zap.Logger) (*Tracker, error) {
	// Validate eBPF event structure matches C struct
	if err := validateBpfConnectionEvent(); err != nil {
		return nil, fmt.Errorf("validating bpfConnectionEvent: %w", err)
	}

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
// Priority: 1) Explicit path via flag, 2) Embedded version, 3) Simulation
func (t *Tracker) loadEBPF() error {
	// Priority 1: Explicit path via flag
	if t.config.EBPFProgramPath != "" {
		t.logger.Info("Loading eBPF from specified path",
			zap.String("path", t.config.EBPFProgramPath))
		return t.loadEBPFFromFile(t.config.EBPFProgramPath)
	}

	// Priority 2: Embedded version (always used by default)
	if embedded.HasEmbeddedEBPF() {
		t.logger.Info("Using embedded eBPF program")
		return t.loadEmbeddedEBPF()
	}

	// Priority 3: Build without embed (simulation)
	t.logger.Info("No eBPF program available, using simulated events")
	go t.simulateEvents()
	return nil
}

// loadEmbeddedEBPF loads eBPF from embedded resources
func (t *Tracker) loadEmbeddedEBPF() error {
	// Create temp file
	tmpFile, err := os.CreateTemp("", "conntrack-ebpf-*.o")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	data, err := embedded.GetEBPFProgram()
	if err != nil {
		return fmt.Errorf("getting embedded eBPF: %w", err)
	}

	if _, err := tmpFile.Write(data); err != nil {
		return fmt.Errorf("writing eBPF: %w", err)
	}
	tmpFile.Close()

	return t.loadEBPFFromFile(tmpFile.Name())
}

// loadEBPFFromFile loads eBPF from a file path
func (t *Tracker) loadEBPFFromFile(path string) error {
	t.logger.Info("Loading eBPF program", zap.String("path", path))

	// Load eBPF collection spec from ELF file
	spec, err := ebpf.LoadCollectionSpec(path)
	if err != nil {
		return fmt.Errorf("loading collection spec from %s: %w", path, err)
	}

	// Log available programs and maps
	t.logger.Debug("eBPF spec loaded",
		zap.Strings("programs", getMapKeys(spec.Programs)),
		zap.Strings("maps", getMapKeys2(spec.Maps)),
	)

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

// getMapKeys returns keys from a map as string slice
func getMapKeys(m map[string]*ebpf.Program) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func getMapKeys2(m map[string]*ebpf.MapSpec) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// attachPrograms attaches eBPF programs to kernel hooks
// Uses auto-detection: tries kprobe first, falls back to tracepoint if kprobe doesn't fire
func (t *Tracker) attachPrograms() error {
	// Log available programs
	t.logger.Info("Available eBPF programs",
		zap.Strings("programs", getMapKeys(t.colls.Programs)),
	)

	// Attach tcp_connect for outgoing connections
	// On kernels 6.1.x (Debian 12), kprobe/tcp_connect may attach but not fire
	// In this case, tracepoint/sock/inet_sock_set_state is used as fallback
	if t.config.TrackOutgoing {
		kprobeAttached := false
		tracepointAttached := false

		// Try kprobe first
		if prog, ok := t.colls.Programs["tcp_connect"]; ok {
			l, err := link.Kprobe("tcp_connect", prog, nil)
			if err != nil {
				t.logger.Warn("Failed to attach kprobe/tcp_connect", zap.Error(err))
			} else {
				t.links = append(t.links, l)
				t.logger.Info("Attached kprobe/tcp_connect for outgoing connections")
				kprobeAttached = true
			}
		}

		// Try tracepoint fallback
		// Always attach if kprobe failed, or as additional coverage on problematic kernels
		if prog, ok := t.colls.Programs["trace_outgoing_fallback"]; ok {
			l, err := link.Tracepoint("sock", "inet_sock_set_state", prog, nil)
			if err != nil {
				if !kprobeAttached {
					return fmt.Errorf("kprobe failed and tracepoint fallback also failed: %w", err)
				}
				t.logger.Debug("Tracepoint fallback not available (kprobe is active)", zap.Error(err))
			} else {
				t.links = append(t.links, l)
				if !kprobeAttached {
					t.logger.Info("Attached tracepoint/sock/inet_sock_set_state for outgoing connections (fallback mode)")
				} else {
					t.logger.Info("Attached tracepoint/sock/inet_sock_set_state for outgoing connections (dual mode)")
				}
				tracepointAttached = true
			}
		}

		// Error if neither kprobe nor tracepoint was attached
		if !kprobeAttached && !tracepointAttached {
			return fmt.Errorf("failed to attach any outgoing connection tracker (kprobe and tracepoint both unavailable)")
		}
	}

	// Attach inet_csk_accept for incoming connections (kretprobe)
	if t.config.TrackIncoming {
		if prog, ok := t.colls.Programs["inet_csk_accept"]; ok {
			l, err := link.Kretprobe("inet_csk_accept", prog, nil)
			if err != nil {
				t.logger.Warn("Failed to attach kretprobe/inet_csk_accept", zap.Error(err))
			} else {
				t.links = append(t.links, l)
				t.logger.Info("Attached kretprobe/inet_csk_accept for incoming connections")
			}
		}
	}

	// Attach tcp_close for connection closing (kprobe, not kretprobe)
	if t.config.TrackCloses {
		if prog, ok := t.colls.Programs["tcp_close"]; ok {
			l, err := link.Kprobe("tcp_close", prog, nil)
			if err != nil {
				return fmt.Errorf("linking kprobe/tcp_close: %w", err)
			}
			t.links = append(t.links, l)
			t.logger.Info("Attached kprobe/tcp_close for connection closing")
		}
	}

	return nil
}

// readEvents reads connection events from eBPF ring buffer
func (t *Tracker) readEvents(ctx context.Context) {
	if t.colls == nil {
		// eBPF not loaded, events will come from simulation
		t.logger.Info("eBPF collection not loaded, using simulation")
		return
	}

	// Get ring buffer reader
	ringBuf, ok := t.colls.Maps["events"]
	if !ok {
		t.logger.Error("Events map not found in eBPF collection")
		return
	}

	t.logger.Info("Creating ringbuf reader", zap.String("map", ringBuf.String()))
	rd, err := ringbuf.NewReader(ringBuf)
	if err != nil {
		t.logger.Error("Creating ringbuf reader", zap.Error(err))
		return
	}
	defer rd.Close()

	t.logger.Info("Ringbuf reader created, starting to read events")

	for {
		select {
		case <-ctx.Done():
			t.logger.Info("Context done, exiting ringbuf reader")
			return
		default:
		}

		record, err := rd.Read()
		if err != nil {
			t.logger.Debug("Reading ringbuf", zap.Error(err))
			continue
		}

		t.logger.Debug("Ringbuf event received", zap.Int("bytes", len(record.RawSample)))

		conn := t.parseConnectionEvent(record.RawSample)
		if conn != nil {
			t.processConnection(conn)
		}
	}
}

// parseConnectionEvent parses raw eBPF event data
func (t *Tracker) parseConnectionEvent(data []byte) *Connection {
	// C struct: 8+8+4+4+16+16+2+2+1+1+1+1+1+7(pad)+16 = 88 bytes
	if len(data) < 88 {
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
	// Skip 7-byte padding (offset 65-71)
	copy(event.Comm[:], data[72:88])

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
		ProcessName: enrichProcessName(string(event.Comm[:]), event.PID),
	}

	// Generate connection ID
	conn.ID = makeConnectionKey(
		conn.SourceIP, conn.SourcePort,
		conn.DestIP, conn.DestPort,
		conn.Protocol,
	)

	// Log all events for debugging (including src_ip=0.0.0.0)
	t.logger.Debug("Parsed eBPF event",
		zap.String("src_ip", conn.SourceIP.String()),
		zap.String("dst_ip", conn.DestIP.String()),
		zap.Uint16("src_port", conn.SourcePort),
		zap.Uint16("dst_port", conn.DestPort),
		zap.String("direction", conn.Direction.String()),
		zap.String("state", conn.State.String()),
		zap.String("event_type", conn.State.String()),
		zap.String("process", conn.ProcessName),
		zap.Uint32("pid", conn.PID),
	)

	return conn
}

// sanitizeProcessName cleans up process name from eBPF
// With proper struct alignment, null bytes should only be at the end
func sanitizeProcessName(name string) string {
	// Remove null bytes and trim whitespace
	name = strings.TrimRight(name, "\x00")
	name = strings.TrimSpace(name)
	if name == "" {
		return "unknown"
	}
	return name
}

// getProcessComm reads process name from /proc/{pid}/comm
// Used to enrich comm field when eBPF kretprobe returns empty/invalid comm
// Returns empty string if PID is 0 or /proc/{pid}/comm is not accessible
func getProcessComm(pid uint32) string {
	if pid == 0 {
		return ""
	}

	commPath := fmt.Sprintf("/proc/%d/comm", pid)
	data, err := os.ReadFile(commPath)
	if err != nil {
		// Process may have exited or permissions issue
		return ""
	}

	// /proc/{pid}/comm contains just the process name + newline
	name := strings.TrimSpace(string(data))
	name = strings.TrimRight(name, "\x00")
	
	if name == "" {
		return ""
	}
	return name
}

// enrichProcessName attempts to get process name from /proc/{pid}/comm if eBPF comm is empty/invalid
// Returns the original name if it's valid, otherwise tries to read from /proc
func enrichProcessName(ebpfComm string, pid uint32) string {
	// Check if eBPF comm is valid (non-empty after sanitization)
	sanitized := sanitizeProcessName(ebpfComm)
	if sanitized != "unknown" && sanitized != "" {
		return sanitized
	}

	// eBPF comm was empty or invalid - try /proc/{pid}/comm lookup
	procComm := getProcessComm(pid)
	if procComm != "" {
		return procComm
	}

	return "unknown"
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
	// Create human-readable connection key for logging
	connKey := fmt.Sprintf("%s:%d -> %s:%d (%s)",
		conn.SourceIP.String(), conn.SourcePort,
		conn.DestIP.String(), conn.DestPort,
		conn.Direction.String())

	t.logger.Debug("Connection event",
		zap.String("event", event.String()),
		zap.String("conn", connKey),
		zap.String("source", conn.SourceIP.String()),
		zap.String("dest", conn.DestIP.String()),
		zap.Uint16("src_port", conn.SourcePort),
		zap.Uint16("dst_port", conn.DestPort),
		zap.String("direction", conn.Direction.String()),
		zap.String("state", conn.State.String()),
		zap.String("process", conn.ProcessName),
		zap.Uint32("pid", conn.PID),
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
