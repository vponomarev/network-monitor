package conntrack

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"
)

// ConnectionState represents the state of a TCP connection
type ConnectionState int

const (
	// StateNew - connection just created
	StateNew ConnectionState = iota
	// StateSynSent - SYN sent (outgoing), waiting for SYN+ACK
	StateSynSent
	// StateSynReceived - SYN received (incoming), waiting for accept
	StateSynReceived
	// StateEstablished - connection established (SYN+ACK received or accept called)
	StateEstablished
	// StateClosing - connection closing (FIN sent)
	StateClosing
	// StateClosed - connection closed
	StateClosed
)

// String returns string representation of connection state
func (s ConnectionState) String() string {
	switch s {
	case StateNew:
		return "NEW"
	case StateSynSent:
		return "SYN_SENT"
	case StateSynReceived:
		return "SYN_RECEIVED"
	case StateEstablished:
		return "ESTABLISHED"
	case StateClosing:
		return "CLOSING"
	case StateClosed:
		return "CLOSED"
	default:
		return "UNKNOWN"
	}
}

// ConnectionEvent represents the type of connection event
type ConnectionEvent int

const (
	// EventNew - new connection detected
	EventNew ConnectionEvent = iota
	// EventEstablished - connection established
	EventEstablished
	// EventClosed - connection closed
	EventClosed
	// EventFailed - connection failed (SYN without SYN+ACK)
	EventFailed
	// EventRejected - incoming connection rejected
	EventRejected
)

// String returns string representation of connection event
func (e ConnectionEvent) String() string {
	switch e {
	case EventNew:
		return "NEW"
	case EventEstablished:
		return "ESTABLISHED"
	case EventClosed:
		return "CLOSED"
	case EventFailed:
		return "FAILED"
	case EventRejected:
		return "REJECTED"
	default:
		return "UNKNOWN"
	}
}

// StateMachineConfig holds configuration for the state machine
type StateMachineConfig struct {
	// SYNTimeout is the timeout for waiting SYN+ACK (default: 30s)
	SYNTimeout time.Duration

	// CleanupDelay is the delay before removing closed connections (default: 5m)
	CleanupDelay time.Duration

	// RetentionTTL bounds state when a CLOSE event is lost.
	RetentionTTL time.Duration

	// CleanupInterval controls retention sweeps.
	CleanupInterval time.Duration

	// MaxConnections is a hard userspace state limit.
	MaxConnections int

	// OnStateChange is called when connection state changes
	OnStateChange func(*Connection, ConnectionState, ConnectionState)

	// OnEvent is called when connection event occurs
	OnEvent func(*Connection, ConnectionEvent)

	// OnCleanup reports state removed by retention or capacity enforcement.
	OnCleanup func(reason string, count int)
}

const (
	CleanupReasonClosed     = "closed"
	CleanupReasonTTL        = "ttl"
	CleanupReasonSYNTimeout = "syn_timeout"
	CleanupReasonCapacity   = "capacity"
)

// StateMachine manages TCP connection state transitions
type StateMachine struct {
	mu sync.RWMutex

	// Active connections
	connections map[string]*Connection

	// Timeout for SYN_SENT state (no SYN+ACK received)
	synTimeout time.Duration

	// Delay before removing closed connections
	cleanupDelay    time.Duration
	retentionTTL    time.Duration
	cleanupInterval time.Duration
	maxConnections  int

	// Context for shutdown
	ctx    context.Context
	cancel context.CancelFunc

	// WaitGroup for background goroutines
	wg sync.WaitGroup

	// Callbacks
	onStateChange func(*Connection, ConnectionState, ConnectionState)
	onEvent       func(*Connection, ConnectionEvent)
	onCleanup     func(reason string, count int)
}

// NewStateMachine creates a new connection state machine
func NewStateMachine(cfg StateMachineConfig) *StateMachine {
	if cfg.SYNTimeout == 0 {
		cfg.SYNTimeout = 30 * time.Second
	}
	if cfg.CleanupDelay == 0 {
		cfg.CleanupDelay = 5 * time.Minute
	}
	if cfg.RetentionTTL <= 0 {
		cfg.RetentionTTL = DefaultStateTTL
	}
	if cfg.CleanupInterval <= 0 {
		cfg.CleanupInterval = DefaultCleanupInterval
	}
	if cfg.MaxConnections <= 0 {
		cfg.MaxConnections = DefaultMaxTrackedConnections
	}

	ctx, cancel := context.WithCancel(context.Background())

	sm := &StateMachine{
		connections:     make(map[string]*Connection),
		synTimeout:      cfg.SYNTimeout,
		cleanupDelay:    cfg.CleanupDelay,
		retentionTTL:    cfg.RetentionTTL,
		cleanupInterval: cfg.CleanupInterval,
		maxConnections:  cfg.MaxConnections,
		onStateChange:   cfg.OnStateChange,
		onEvent:         cfg.OnEvent,
		onCleanup:       cfg.OnCleanup,
		ctx:             ctx,
		cancel:          cancel,
	}

	// Start timeout checker
	sm.wg.Add(1)
	go sm.checkTimeouts()
	sm.wg.Add(1)
	go sm.cleanupLoop()

	return sm
}

// ProcessEvent processes an incoming connection event
func (sm *StateMachine) ProcessEvent(evt *ConnectionEventRaw) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	key := sm.makeKey(evt)
	conn, exists := sm.connections[key]

	switch evt.EventType {
	case EventNew:
		if !exists {
			conn = sm.createConnection(evt)
		}
		sm.transitionState(conn, StateSynSent)
		sm.emitEvent(conn, EventNew)

	case EventEstablished:
		if exists && conn.State == StateEstablished {
			return
		}
		if !exists {
			// SYN+ACK without seeing SYN - create new connection
			conn = sm.createConnection(evt)
		}
		conn.SynAckReceived = true
		conn.Established = true
		conn.EstablishedTime = time.Now()
		sm.transitionState(conn, StateEstablished)
		sm.emitEvent(conn, EventEstablished)

	case EventClosed:
		if exists {
			if conn.State == StateClosed {
				return
			}
			conn.ClosedTime = time.Now()
			sm.transitionState(conn, StateClosed)
			sm.emitEvent(conn, EventClosed)
		}

	case EventFailed:
		if exists {
			sm.transitionState(conn, StateClosed)
			sm.emitEvent(conn, EventFailed)
			delete(sm.connections, key)
		}

	case EventRejected:
		if exists {
			sm.transitionState(conn, StateClosed)
			sm.emitEvent(conn, EventRejected)
			delete(sm.connections, key)
		}
	}
}

// createConnection creates a new connection from event
func (sm *StateMachine) createConnection(evt *ConnectionEventRaw) *Connection {
	if len(sm.connections) >= sm.maxConnections {
		sm.evictOldest()
	}
	now := time.Now()
	conn := &Connection{
		ID:          sm.makeKey(evt),
		SourceIP:    evt.SourceIP,
		SourcePort:  evt.SourcePort,
		DestIP:      evt.DestIP,
		DestPort:    evt.DestPort,
		Protocol:    evt.Protocol,
		Direction:   evt.Direction,
		State:       StateNew,
		Timestamp:   now,
		LastUpdated: now,
		PID:         evt.PID,
		ProcessName: evt.ProcessName,
		SynSentTime: now,
	}

	sm.connections[conn.ID] = conn
	return conn
}

func (sm *StateMachine) evictOldest() {
	var oldestKey string
	var oldestTime time.Time
	for key, conn := range sm.connections {
		updated := conn.LastUpdated
		if updated.IsZero() {
			updated = conn.Timestamp
		}
		if oldestKey == "" || updated.Before(oldestTime) {
			oldestKey = key
			oldestTime = updated
		}
	}
	if oldestKey != "" {
		delete(sm.connections, oldestKey)
		sm.notifyCleanup(CleanupReasonCapacity, 1)
	}
}

// transitionState transitions connection to new state
func (sm *StateMachine) transitionState(conn *Connection, newState ConnectionState) {
	oldState := conn.State
	conn.State = newState
	conn.LastUpdated = time.Now()

	if sm.onStateChange != nil {
		sm.onStateChange(conn, oldState, newState)
	}
}

// emitEvent emits connection event
func (sm *StateMachine) emitEvent(conn *Connection, event ConnectionEvent) {
	if sm.onEvent != nil {
		sm.onEvent(conn, event)
	}
}

// checkTimeouts periodically checks for timed out connections
func (sm *StateMachine) checkTimeouts() {
	defer sm.wg.Done()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-sm.ctx.Done():
			return
		case <-ticker.C:
			sm.mu.Lock()
			now := time.Now()
			removed := 0

			for key, conn := range sm.connections {
				if conn.State == StateSynSent {
					if now.Sub(conn.SynSentTime) > sm.synTimeout {
						// SYN timeout - connection failed
						conn.State = StateClosed
						conn.ClosedTime = now
						sm.emitEvent(conn, EventFailed)
						delete(sm.connections, key)
						removed++
					}
				}
			}
			sm.mu.Unlock()
			sm.notifyCleanup(CleanupReasonSYNTimeout, removed)
		}
	}
}

func (sm *StateMachine) cleanupLoop() {
	defer sm.wg.Done()
	ticker := time.NewTicker(sm.cleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-sm.ctx.Done():
			return
		case now := <-ticker.C:
			sm.cleanup(now)
		}
	}
}

func (sm *StateMachine) cleanup(now time.Time) {
	counts := map[string]int{}
	sm.mu.Lock()
	for key, conn := range sm.connections {
		if conn.State == StateClosed && !conn.ClosedTime.IsZero() && now.Sub(conn.ClosedTime) >= sm.cleanupDelay {
			delete(sm.connections, key)
			counts[CleanupReasonClosed]++
			continue
		}
		updated := conn.LastUpdated
		if updated.IsZero() {
			updated = conn.Timestamp
		}
		if !updated.IsZero() && now.Sub(updated) >= sm.retentionTTL {
			delete(sm.connections, key)
			counts[CleanupReasonTTL]++
		}
	}
	sm.mu.Unlock()
	for reason, count := range counts {
		sm.notifyCleanup(reason, count)
	}
}

func (sm *StateMachine) notifyCleanup(reason string, count int) {
	if count > 0 && sm.onCleanup != nil {
		sm.onCleanup(reason, count)
	}
}

// Stop gracefully stops the state machine and all background goroutines
func (sm *StateMachine) Stop() {
	sm.cancel()
	sm.wg.Wait()
}

// makeKey generates unique key for connection
func (sm *StateMachine) makeKey(evt *ConnectionEventRaw) string {
	return makeConnectionKey(
		evt.SourceIP, evt.SourcePort,
		evt.DestIP, evt.DestPort,
		evt.Protocol,
	)
}

// GetConnection returns connection by key
func (sm *StateMachine) GetConnection(key string) (*Connection, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	conn, exists := sm.connections[key]
	if !exists {
		return nil, false
	}
	return cloneConnection(conn), true
}

// GetAllConnections returns all active connections
func (sm *StateMachine) GetAllConnections() []*Connection {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	conns := make([]*Connection, 0, len(sm.connections))
	for _, conn := range sm.connections {
		conns = append(conns, cloneConnection(conn))
	}
	return conns
}

func cloneConnection(conn *Connection) *Connection {
	if conn == nil {
		return nil
	}
	copyConn := *conn
	copyConn.SourceIP = append(net.IP(nil), conn.SourceIP...)
	copyConn.DestIP = append(net.IP(nil), conn.DestIP...)
	return &copyConn
}

// GetConnectionCount returns number of tracked connections
func (sm *StateMachine) GetConnectionCount() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return len(sm.connections)
}

// GetStats returns connection statistics
func (sm *StateMachine) GetStats() Stats {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	stats := Stats{}
	for _, conn := range sm.connections {
		switch conn.State {
		case StateSynSent:
			stats.PendingOutgoing++
		case StateSynReceived:
			stats.PendingIncoming++
		case StateEstablished:
			stats.Established++
		}

		if conn.Direction == DirectionOutgoing {
			stats.TotalOutgoing++
		} else {
			stats.TotalIncoming++
		}
	}

	return stats
}

// Stats holds connection statistics
type Stats struct {
	TotalOutgoing   int
	TotalIncoming   int
	PendingOutgoing int // SYN sent, waiting SYN+ACK
	PendingIncoming int // SYN received, waiting accept
	Established     int
}

// ConnectionEventRaw represents raw connection event from eBPF
type ConnectionEventRaw struct {
	SourceIP    net.IP
	SourcePort  uint16
	DestIP      net.IP
	DestPort    uint16
	Protocol    uint8
	Direction   Direction
	EventType   ConnectionEvent
	State       ConnectionState
	PID         uint32
	ProcessName string
	Timestamp   time.Time
}

// makeConnectionKey generates unique key for connection
func makeConnectionKey(srcIP net.IP, srcPort uint16, dstIP net.IP, dstPort uint16, protocol uint8) string {
	return fmt.Sprintf("%s:%d-%s:%d-%d",
		srcIP.String(), srcPort,
		dstIP.String(), dstPort,
		protocol)
}
