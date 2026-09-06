//go:build linux
// +build linux

package conntrack

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/vponomarev/network-monitor/pkg/embedded"
	"go.uber.org/zap"
	"golang.org/x/sys/unix"
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
	syslogEvents chan syslogEvent

	// Metrics collector
	metricsCollector *MetricsCollector

	wg sync.WaitGroup

	// Event channel
	events        chan *Connection
	eventsEnabled atomic.Bool

	// Dropped events counter
	droppedEvents             uint64
	syslogDropped             uint64
	kernelDroppedEvents       uint64
	kernelConnectionOverflows uint64
	kernelPendingOverflows    uint64
	ready                     atomic.Bool
}

type syslogEvent struct {
	conn  *Connection
	event ConnectionEvent
}

// eBPF event structure (must match C struct)
// C struct has __u8 _pad[7] after tcp_flags for 8-byte alignment of comm
type bpfConnectionEvent struct {
	TimestampNs uint64   // offset 0
	PidTgid     uint64   // offset 8
	PID         uint32   // offset 16
	TID         uint32   // offset 20
	SrcIP       [16]byte // offset 24
	DstIP       [16]byte // offset 40
	SrcPort     uint16   // offset 56
	DstPort     uint16   // offset 58
	Protocol    uint8    // offset 60
	Direction   uint8    // offset 61
	State       uint8    // offset 62
	EventType   uint8    // offset 63
	TCPFlags    uint8    // offset 64
	_           [7]byte  // offset 65-71 (padding for comm alignment)
	Comm        [16]byte // offset 72
	SocketID    uint64
	StartedNS   uint64
	HandshakeNS uint64
}

// validateBpfConnectionEvent checks that Go struct matches C struct
func validateBpfConnectionEvent() error {
	// C struct: 8+8+4+4+16+16+2+2+1+1+1+1+1+7(pad)+16 = 88 bytes
	if unsafe.Sizeof(bpfConnectionEvent{}) != 112 {
		return fmt.Errorf("bpfConnectionEvent size mismatch: got %d, expected 112",
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

	if cfg.StateTTL <= 0 {
		cfg.StateTTL = DefaultStateTTL
	}
	if cfg.CleanupInterval <= 0 {
		cfg.CleanupInterval = DefaultCleanupInterval
	}
	if cfg.MaxTrackedConnections <= 0 {
		cfg.MaxTrackedConnections = DefaultMaxTrackedConnections
	}
	if cfg.MaxPendingConnections <= 0 {
		cfg.MaxPendingConnections = DefaultMaxPendingConnections
	}
	if int64(cfg.MaxTrackedConnections) > int64(^uint32(0)) || int64(cfg.MaxPendingConnections) > int64(^uint32(0)) {
		return nil, fmt.Errorf("conntrack map limits exceed uint32 capacity")
	}

	// Set default buffer size if not specified
	bufferSize := cfg.EventBufferSize
	if bufferSize <= 0 {
		bufferSize = DefaultEventBufferSize
	}

	tracker := &Tracker{
		config: cfg,
		logger: logger.Named("conntrack"),
		events: make(chan *Connection, bufferSize),
	}

	// Log buffer size
	logger.Info("Connection tracker buffer size", zap.Int("size", bufferSize))

	// Create metrics collector
	tracker.metricsCollector = NewMetricsCollectorWithRegisterer(logger, cfg.Registerer)

	// Create state machine
	tracker.stateMachine = NewStateMachine(StateMachineConfig{
		SYNTimeout:      cfg.SYNTimeout,
		RetentionTTL:    cfg.StateTTL,
		CleanupInterval: cfg.CleanupInterval,
		MaxConnections:  cfg.MaxTrackedConnections,
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
		OnCleanup: func(reason string, count int) {
			tracker.metricsCollector.AddCleanup(reason, count)
			if reason == CleanupReasonCapacity && count > 0 {
				tracker.metricsCollector.AddEviction("userspace", count)
				// #nosec G115 -- count is positive and bounded by MaxTrackedConnections.
				tracker.metricsCollector.AddOverflow("userspace", uint64(count))
			}
		},
	})

	// Create syslog writer if configured
	if cfg.Syslog.Tag != "" {
		writer, err := NewSyslogWriter(cfg.Syslog)
		if err != nil {
			logger.Warn("Failed to create syslog writer", zap.Error(err))
		} else {
			tracker.syslogWriter = writer
			tracker.syslogEvents = make(chan syslogEvent, 1024)
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
	defer t.ready.Store(false)
	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()

	// Create the reader before advertising readiness. A missing/incompatible map
	// is a startup failure, not a healthy daemon with a dead consumer.
	if t.colls == nil {
		return fmt.Errorf("eBPF collection is nil after load")
	}
	ringBuf, ok := t.colls.Maps["events"]
	if !ok {
		return fmt.Errorf("events map not found in eBPF collection")
	}
	rd, err := ringbuf.NewReader(ringBuf)
	if err != nil {
		return fmt.Errorf("creating ringbuf reader: %w", err)
	}

	// Start background consumers and wait for them before closing eBPF maps.
	readerErr := make(chan error, 1)
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		readerErr <- t.readEvents(runCtx, rd)
	}()
	if t.syslogEvents != nil {
		t.wg.Add(1)
		go func() {
			defer t.wg.Done()
			t.writeSyslog(runCtx)
		}()
	}
	t.ready.Store(true)
	if dropMap, ok := t.colls.Maps["event_drops"]; ok {
		t.wg.Add(1)
		go func() {
			defer t.wg.Done()
			t.observeKernelDrops(runCtx, dropMap)
		}()
	} else {
		t.logger.Warn("Kernel drop counter map is unavailable; rebuild conntrack eBPF")
	}
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		t.cleanupKernelState(runCtx)
	}()
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		t.updateMetrics(runCtx)
	}()

	var runErr error
	readerStopped := false
	select {
	case <-ctx.Done():
	case err := <-readerErr:
		readerStopped = true
		if err != nil {
			runErr = fmt.Errorf("ringbuf consumer stopped: %w", err)
		}
	}
	cancelRun()
	if t.syslogWriter != nil && t.syslogWriter.remote != nil {
		_ = t.syslogWriter.Close()
	}
	if !readerStopped {
		if err := <-readerErr; err != nil && ctx.Err() == nil {
			runErr = fmt.Errorf("ringbuf consumer stopped: %w", err)
		}
	}
	if t.syslogEvents != nil {
		t.stateMachine.Stop() // Stop timeout callbacks before closing their queue.
		close(t.syslogEvents)
	}
	t.ready.Store(false)
	t.logger.Info("Stopping connection tracker")
	t.wg.Wait()
	return runErr
}

// loadEBPF loads and attaches eBPF programs
// Priority: 1) Explicit path via flag, 2) Embedded version.
// Production must fail closed when no real eBPF program is available.
func (t *Tracker) loadEBPF() error {
	// Raising the memlock limit is a kernel-facing startup operation. Keep it
	// out of NewTracker so callers can construct and inspect userspace state
	// without eBPF privileges (for example API handlers and unit tests).
	if err := rlimit.RemoveMemlock(); err != nil {
		return fmt.Errorf("removing memlock: %w", err)
	}

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

	return fmt.Errorf("no eBPF program available: specify --ebpf-prog or use a build with embedded eBPF")
}

// loadEmbeddedEBPF loads eBPF from embedded resources
func (t *Tracker) loadEmbeddedEBPF() error {
	data, err := embedded.GetEBPFProgram()
	if err != nil {
		return fmt.Errorf("getting embedded eBPF: %w", err)
	}
	spec, err := ebpf.LoadCollectionSpecFromReader(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("loading embedded collection spec: %w", err)
	}
	return t.loadEBPFSpec(spec)
}

// loadEBPFFromFile loads eBPF from a file path
func (t *Tracker) loadEBPFFromFile(path string) error {
	t.logger.Info("Loading eBPF program", zap.String("path", path))

	// Load eBPF collection spec from ELF file
	spec, err := ebpf.LoadCollectionSpec(path)
	if err != nil {
		return fmt.Errorf("loading collection spec from %s: %w", path, err)
	}
	return t.loadEBPFSpec(spec)
}

func (t *Tracker) loadEBPFSpec(spec *ebpf.CollectionSpec) error {
	if m := spec.Maps["connections"]; m == nil || m.KeySize != 45 {
		return fmt.Errorf("incompatible conntrack eBPF ABI: rebuild the object with this binary")
	}
	if connections, ok := spec.Maps["connections"]; ok {
		// #nosec G115 -- NewTracker rejects values outside uint32 above.
		connections.MaxEntries = uint32(t.config.MaxTrackedConnections)
	}
	if pending, ok := spec.Maps["pending_outgoing"]; ok {
		// #nosec G115 -- NewTracker rejects values outside uint32 above.
		pending.MaxEntries = uint32(t.config.MaxPendingConnections)
	}

	// Log available programs and maps
	t.logger.Debug("eBPF spec loaded",
		zap.Strings("programs", mapKeys(spec.Programs)),
		zap.Strings("maps", mapKeys(spec.Maps)),
	)

	// Check if tracepoint is present - may not be supported on older kernels
	hasTracepoint := false
	if _, ok := spec.Programs["trace_outgoing"]; ok {
		hasTracepoint = true
		t.logger.Debug("trace_outgoing program found in spec")
	}

	// Try to load collection with tracepoint first
	colls, err := ebpf.NewCollection(spec)
	if err != nil {
		// If tracepoint is present and load fails, try removing it (fallback to kprobe)
		if hasTracepoint {
			t.logger.Debug("Failed to load with tracepoint, trying without", zap.Error(err))
			delete(spec.Programs, "trace_outgoing")
			colls, err = ebpf.NewCollection(spec)
			if err != nil {
				return fmt.Errorf("creating eBPF collection (without tracepoint): %w", err)
			}
			t.logger.Info("eBPF collection loaded without tracepoint (kprobe fallback)")
		} else {
			return fmt.Errorf("creating eBPF collection: %w", err)
		}
	}
	t.colls = colls
	if err := t.configurePortFilter(); err != nil {
		t.colls.Close()
		t.colls = nil
		return fmt.Errorf("configuring port filter: %w", err)
	}

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

func (t *Tracker) configurePortFilter() error {
	configMap, ok := t.colls.Maps["filter_config"]
	if !ok {
		if len(t.config.FilterPorts) > 0 {
			return fmt.Errorf("filter_config map is unavailable; rebuild conntrack eBPF")
		}
		return nil
	}
	portsMap, ok := t.colls.Maps["filter_ports"]
	if !ok {
		return fmt.Errorf("filter_ports map is unavailable")
	}
	enabled := uint8(0)
	if len(t.config.FilterPorts) > 0 {
		enabled = 1
		for _, configured := range t.config.FilterPorts {
			// #nosec G115 -- configuration validation restricts ports to 1..65535.
			port, value := uint16(configured), uint8(1)
			if err := portsMap.Put(port, value); err != nil {
				return fmt.Errorf("adding port %d: %w", configured, err)
			}
		}
	}
	if err := configMap.Put(uint32(0), enabled); err != nil {
		return fmt.Errorf("setting filter state: %w", err)
	}
	return nil
}

func mapKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// attachPrograms attaches eBPF programs to kernel hooks
// Uses inet_sock_set_state and correlates SYN_SENT process metadata with the
// complete ESTABLISHED tuple.
func (t *Tracker) attachPrograms() error {
	// Log available programs
	t.logger.Info("Available eBPF programs",
		zap.Strings("programs", mapKeys(t.colls.Programs)),
	)

	if t.config.TrackOutgoing || t.config.TrackIncoming || t.config.TrackCloses {
		prog, ok := t.colls.Programs["trace_outgoing"]
		if !ok {
			return fmt.Errorf("outgoing inet_sock_set_state program is missing")
		}
		outgoingLink, err := link.Tracepoint("sock", "inet_sock_set_state", prog, nil)
		if err != nil {
			return fmt.Errorf("linking tracepoint/sock/inet_sock_set_state: %w", err)
		}
		t.links = append(t.links, outgoingLink)
		t.logger.Info("Attached inet_sock_set_state tracepoint for outgoing connections")
	}

	// Attach inet_csk_accept for incoming connections (kretprobe)
	if t.config.TrackIncoming {
		if prog, ok := t.colls.Programs["inet_csk_accept"]; ok {
			l, err := link.Kretprobe("inet_csk_accept", prog, nil)
			if err != nil {
				return fmt.Errorf("linking kretprobe/inet_csk_accept: %w", err)
			} else {
				t.links = append(t.links, l)
				t.logger.Info("Attached kretprobe/inet_csk_accept for incoming connections")
			}
		} else {
			return fmt.Errorf("incoming inet_csk_accept program is missing")
		}
	}

	return nil
}

// readEvents reads connection events from eBPF ring buffer
func (t *Tracker) readEvents(ctx context.Context, rd *ringbuf.Reader) error {
	defer rd.Close()
	go func() {
		<-ctx.Done()
		rd.Close()
	}()

	t.logger.Info("Ringbuf reader created, starting to read events")

	for {
		select {
		case <-ctx.Done():
			t.logger.Info("Context done, exiting ringbuf reader")
			return nil
		default:
		}

		record, err := rd.Read()
		if err != nil {
			if ctx.Err() != nil {
				t.logger.Info("Ringbuf reader stopped")
				return nil
			}
			if errors.Is(err, ringbuf.ErrClosed) {
				return fmt.Errorf("ringbuf closed unexpectedly: %w", err)
			}
			return fmt.Errorf("reading ringbuf: %w", err)
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
	if len(data) != 112 {
		t.logger.Debug("Event data too short", zap.Int("len", len(data)))
		return nil
	}

	// Parse binary data (must match C struct layout)
	event := &bpfConnectionEvent{}
	event.TimestampNs = binary.NativeEndian.Uint64(data[0:8])
	event.PidTgid = binary.NativeEndian.Uint64(data[8:16])
	event.PID = binary.NativeEndian.Uint32(data[16:20])
	event.TID = binary.NativeEndian.Uint32(data[20:24])
	copy(event.SrcIP[:], data[24:40])
	copy(event.DstIP[:], data[40:56])
	event.SrcPort = binary.NativeEndian.Uint16(data[56:58])
	event.DstPort = binary.NativeEndian.Uint16(data[58:60])
	event.Protocol = data[60]
	event.Direction = data[61]
	event.State = data[62]
	event.EventType = data[63]
	event.TCPFlags = data[64]
	// Skip 7-byte padding (offset 65-71)
	copy(event.Comm[:], data[72:88])

	// Convert to Connection
	conn := &Connection{
		// bpf_ktime_get_ns is monotonic since boot, not a Unix timestamp.
		// Use wall time at ingestion for logs and userspace retention.
		Timestamp:         kernelEventTime(event.TimestampNs),
		SocketID:          binary.NativeEndian.Uint64(data[88:96]),
		StartedNS:         binary.NativeEndian.Uint64(data[96:104]),
		MeasuredHandshake: boundedKernelDuration(binary.NativeEndian.Uint64(data[104:112])),
		EventType:         ConnectionEvent(event.EventType),
		SourceIP:          IPFromBytes(event.SrcIP),
		SourcePort:        event.SrcPort,
		DestIP:            IPFromBytes(event.DstIP),
		DestPort:          event.DstPort,
		Protocol:          event.Protocol,
		Direction:         Direction(event.Direction),
		State:             ConnectionState(event.State),
		PID:               event.PID,
		// comm is captured in eBPF while the originating process context is
		// available. Never block the ring-buffer consumer on /proc I/O.
		ProcessName: sanitizeProcessName(string(event.Comm[:])),
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

// processConnection processes a connection event through state machine
func (t *Tracker) processConnection(conn *Connection) {
	if len(t.config.FilterPorts) > 0 && !containsPort(t.config.FilterPorts, conn.SourcePort, conn.DestPort) {
		return
	}
	// Create raw event for state machine
	evt := &ConnectionEventRaw{
		SourceIP:    conn.SourceIP,
		SourcePort:  conn.SourcePort,
		DestIP:      conn.DestIP,
		DestPort:    conn.DestPort,
		Protocol:    conn.Protocol,
		Direction:   conn.Direction,
		EventType:   conn.EventType,
		SocketID:    conn.SocketID,
		StartedNS:   conn.StartedNS,
		Handshake:   conn.MeasuredHandshake,
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

	// Queue syslog I/O so a slow remote collector cannot stall ring-buffer
	// consumption. The queue is bounded and drops are observable.
	if t.syslogEvents != nil {
		select {
		case t.syslogEvents <- syslogEvent{conn: cloneConnection(conn), event: event}:
		default:
			dropped := atomic.AddUint64(&t.syslogDropped, 1)
			t.metricsCollector.UpdateDroppedMetrics("syslog_queue_full", dropped)
		}
	}

	// The standalone service has no event-channel consumer. Enable this optional
	// subscriber queue only after a caller explicitly requests Events().
	if !t.eventsEnabled.Load() {
		return
	}

	// Send to event channel
	eventConn := cloneConnection(conn)
	select {
	case t.events <- eventConn:
	default:
		// Increment dropped counter atomically
		atomic.AddUint64(&t.droppedEvents, 1)
		t.logger.Debug("Event channel full, dropping event",
			zap.Uint64("dropped_total", atomic.LoadUint64(&t.droppedEvents)))
	}
}

func containsPort(ports []int, src, dst uint16) bool {
	for _, port := range ports {
		if port > 0 && port <= 65535 && (uint16(port) == src || uint16(port) == dst) {
			return true
		}
	}
	return false
}

func (t *Tracker) writeSyslog(ctx context.Context) {
	for item := range t.syslogEvents {
		if ctx.Err() != nil {
			dropped := 1
			for range t.syslogEvents {
				dropped++
			}
			t.metricsCollector.UpdateDroppedMetrics("syslog_shutdown", uint64(dropped))
			return
		}
		if err := t.syslogWriter.WriteConnection(item.conn, item.event); err != nil {
			t.logger.Warn("Failed to write to syslog", zap.Error(err))
		}
	}
}

// GetConnections returns all tracked connections
func (t *Tracker) GetConnections() []*Connection {
	return t.stateMachine.GetAllConnections()
}

// GetConnectionCount returns the number of tracked connections
func (t *Tracker) GetConnectionCount() int {
	return t.stateMachine.GetConnectionCount()
}

// GetStats returns connection statistics
func (t *Tracker) GetStats() Stats {
	return t.stateMachine.GetStats()
}

// Events returns the event channel
func (t *Tracker) Events() <-chan *Connection {
	t.eventsEnabled.Store(true)
	return t.events
}

// GetDroppedEvents returns the number of dropped events
func (t *Tracker) GetDroppedEvents() uint64 {
	return atomic.LoadUint64(&t.droppedEvents)
}

// GetKernelDroppedEvents returns events rejected by the kernel ring buffer.
func (t *Tracker) GetKernelDroppedEvents() uint64 {
	return atomic.LoadUint64(&t.kernelDroppedEvents)
}

// Ready reports whether eBPF programs are attached and background consumers
// are running.
func (t *Tracker) Ready() bool { return t.ready.Load() }

func (t *Tracker) observeKernelDrops(ctx context.Context, dropMap *ebpf.Map) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	collectOne := func(key uint32) uint64 {
		var perCPU []uint64
		if err := dropMap.Lookup(&key, &perCPU); err != nil {
			return 0
		}
		var total uint64
		for _, count := range perCPU {
			total += count
		}
		return total
	}
	collect := func() {
		atomic.StoreUint64(&t.kernelDroppedEvents, collectOne(0))
		connections := collectOne(1)
		previousConnections := atomic.SwapUint64(&t.kernelConnectionOverflows, connections)
		if connections >= previousConnections {
			t.metricsCollector.AddOverflow("kernel_connections", connections-previousConnections)
		}
		pending := collectOne(2)
		previousPending := atomic.SwapUint64(&t.kernelPendingOverflows, pending)
		if pending >= previousPending {
			t.metricsCollector.AddOverflow("kernel_pending", pending-previousPending)
		}
		t.metricsCollector.UpdateDroppedMetrics("connections_map_full", connections)
		t.metricsCollector.UpdateDroppedMetrics("pending_map_full", pending)
	}

	collect()
	for {
		select {
		case <-ctx.Done():
			collect()
			return
		case <-ticker.C:
			collect()
		}
	}
}

func (t *Tracker) cleanupKernelState(ctx context.Context) {
	ticker := time.NewTicker(t.config.CleanupInterval)
	defer ticker.Stop()
	for {
		t.cleanupKernelStateOnce()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (t *Tracker) cleanupKernelStateOnce() {
	if t.colls == nil {
		return
	}
	now, err := monotonicNowNS()
	if err != nil {
		t.logger.Warn("Reading monotonic clock for conntrack cleanup", zap.Error(err))
		return
	}
	for _, item := range []struct {
		name  string
		layer string
	}{
		{name: "connections", layer: "kernel_connections"},
		{name: "pending_outgoing", layer: "kernel_pending"},
	} {
		m, ok := t.colls.Maps[item.name]
		if !ok {
			continue
		}
		ttl := t.config.StateTTL
		if item.name == "connections" {
			ttl = 0
		} // Established sockets are removed by CLOSE, never by age.
		remaining, deleted, err := cleanupTimestampedMap(m, now, ttl)
		if err != nil {
			t.logger.Warn("Cleaning conntrack kernel map", zap.String("map", item.name), zap.Error(err))
			continue
		}
		t.metricsCollector.UpdateStateEntries(item.layer, remaining)
		t.metricsCollector.AddCleanup("ttl_"+item.layer, deleted)
	}
}

func monotonicNowNS() (uint64, error) {
	var ts unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &ts); err != nil {
		return 0, err
	}
	// #nosec G115 -- CLOCK_MONOTONIC returns non-negative seconds/nanoseconds.
	return uint64(ts.Sec)*uint64(time.Second) + uint64(ts.Nsec), nil
}

func cleanupTimestampedMap(m *ebpf.Map, nowNS uint64, ttl time.Duration) (remaining, deleted int, err error) {
	iter := m.Iterate()
	var key []byte
	var value []byte
	var expired [][]byte
	// #nosec G115 -- configuration enforces a positive retention TTL.
	cutoff := uint64(ttl)
	for iter.Next(&key, &value) {
		remaining++
		if len(value) < 8 {
			return remaining, deleted, fmt.Errorf("map value too short: %d", len(value))
		}
		timestamp := binary.NativeEndian.Uint64(value[:8])
		if ttl == 0 || timestamp > nowNS || nowNS-timestamp < cutoff {
			continue
		}
		expired = append(expired, append([]byte(nil), key...))
	}
	if err := iter.Err(); err != nil {
		return remaining, deleted, err
	}
	for _, expiredKey := range expired {
		if err := m.Delete(expiredKey); err != nil && !errors.Is(err, ebpf.ErrKeyNotExist) {
			return remaining, deleted, err
		}
		remaining--
		deleted++
	}
	return remaining, deleted, nil
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
				t.metricsCollector.UpdateStateEntries("userspace", t.GetConnectionCount())
				t.metricsCollector.UpdateDroppedMetrics("event_channel_full", t.GetDroppedEvents())
				t.metricsCollector.UpdateDroppedMetrics("ringbuf_full", t.GetKernelDroppedEvents())
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
		t.colls = nil
	}

	// Close syslog writer
	if t.syslogWriter != nil {
		t.syslogWriter.Close()
	}
}

// Translate a monotonic kernel event to wall time, retaining time spent in the ring buffer.
func kernelEventTime(timestamp uint64) time.Time {
	now := time.Now()
	mono, err := monotonicNowNS()
	if err != nil || timestamp > mono {
		return now
	}
	return now.Add(-boundedKernelDuration(mono - timestamp))
}

func boundedKernelDuration(ns uint64) time.Duration {
	if ns > math.MaxInt64 {
		return 0
	}
	return time.Duration(ns)
}
