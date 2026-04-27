package conntrack

import (
	"net"
	"sync/atomic"
	"testing"
	"time"
)

func TestConnectionState_String(t *testing.T) {
	tests := []struct {
		state  ConnectionState
		expect string
	}{
		{StateNew, "NEW"},
		{StateSynSent, "SYN_SENT"},
		{StateSynReceived, "SYN_RECEIVED"},
		{StateEstablished, "ESTABLISHED"},
		{StateClosing, "CLOSING"},
		{StateClosed, "CLOSED"},
		{ConnectionState(999), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.expect, func(t *testing.T) {
			if got := tt.state.String(); got != tt.expect {
				t.Errorf("State.String() = %q, want %q", got, tt.expect)
			}
		})
	}
}

func TestConnectionEvent_String(t *testing.T) {
	tests := []struct {
		event  ConnectionEvent
		expect string
	}{
		{EventNew, "NEW"},
		{EventEstablished, "ESTABLISHED"},
		{EventClosed, "CLOSED"},
		{EventFailed, "FAILED"},
		{EventRejected, "REJECTED"},
		{ConnectionEvent(999), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.expect, func(t *testing.T) {
			if got := tt.event.String(); got != tt.expect {
				t.Errorf("Event.String() = %q, want %q", got, tt.expect)
			}
		})
	}
}

func TestConnection_Duration(t *testing.T) {
	now := time.Now()

	// Active connection
	conn := &Connection{
		Timestamp: now.Add(-10 * time.Second),
	}
	duration := conn.Duration()
	if duration < 9*time.Second || duration > 11*time.Second {
		t.Errorf("Duration() = %v, want ~10s", duration)
	}

	// Closed connection
	conn.ClosedTime = now.Add(-5 * time.Second)
	duration = conn.Duration()
	if duration < 4*time.Second || duration > 6*time.Second {
		t.Errorf("Duration() closed = %v, want ~5s", duration)
	}
}

func TestConnection_HandshakeDuration(t *testing.T) {
	now := time.Now()

	// No handshake data
	conn := &Connection{}
	if got := conn.HandshakeDuration(); got != 0 {
		t.Errorf("HandshakeDuration() empty = %v, want 0", got)
	}

	// With handshake data
	conn.SynSentTime = now
	conn.EstablishedTime = now.Add(50 * time.Millisecond)
	if got := conn.HandshakeDuration(); got != 50*time.Millisecond {
		t.Errorf("HandshakeDuration() = %v, want 50ms", got)
	}
}

func TestConnection_Direction(t *testing.T) {
	conn := &Connection{
		Direction: DirectionOutgoing,
	}

	if !conn.IsOutgoing() {
		t.Error("IsOutgoing() = false, want true")
	}
	if conn.IsIncoming() {
		t.Error("IsIncoming() = true, want false")
	}

	conn.Direction = DirectionIncoming
	if conn.IsOutgoing() {
		t.Error("IsOutgoing() = true, want false")
	}
	if !conn.IsIncoming() {
		t.Error("IsIncoming() = false, want true")
	}
}

func TestStateMachine_NewConnection(t *testing.T) {
	var eventCalled int32
	var stateChangeCalled int32

	sm := NewStateMachine(StateMachineConfig{
		SYNTimeout: 100 * time.Millisecond,
		OnEvent: func(conn *Connection, event ConnectionEvent) {
			atomic.AddInt32(&eventCalled, 1)
		},
		OnStateChange: func(conn *Connection, oldState, newState ConnectionState) {
			atomic.AddInt32(&stateChangeCalled, 1)
		},
	})
	defer sm.Stop()

	// Process new connection event
	evt := &ConnectionEventRaw{
		SourceIP:    net.ParseIP("192.168.1.100"),
		SourcePort:  54321,
		DestIP:      net.ParseIP("8.8.8.8"),
		DestPort:    443,
		Protocol:    6,
		Direction:   DirectionOutgoing,
		EventType:   EventNew,
		State:       StateSynSent,
		PID:         1234,
		ProcessName: "curl",
		Timestamp:   time.Now(),
	}

	sm.ProcessEvent(evt)

	// Wait for callbacks
	time.Sleep(10 * time.Millisecond)

	// Check event was called
	if atomic.LoadInt32(&eventCalled) == 0 {
		t.Error("OnEvent callback not called")
	}

	// Check state change was called
	if atomic.LoadInt32(&stateChangeCalled) == 0 {
		t.Error("OnStateChange callback not called")
	}

	// Check connection is tracked
	stats := sm.GetStats()
	if stats.PendingOutgoing != 1 {
		t.Errorf("GetStats() PendingOutgoing = %d, want 1", stats.PendingOutgoing)
	}
}

func TestStateMachine_EstablishedConnection(t *testing.T) {
	sm := NewStateMachine(StateMachineConfig{
		SYNTimeout: 1 * time.Second,
	})
	defer sm.Stop()

	// Process new connection
	newEvt := &ConnectionEventRaw{
		SourceIP:    net.ParseIP("192.168.1.100"),
		SourcePort:  54321,
		DestIP:      net.ParseIP("8.8.8.8"),
		DestPort:    443,
		Protocol:    6,
		Direction:   DirectionOutgoing,
		EventType:   EventNew,
		Timestamp:   time.Now(),
	}
	sm.ProcessEvent(newEvt)

	// Process established event
	estEvt := &ConnectionEventRaw{
		SourceIP:    net.ParseIP("192.168.1.100"),
		SourcePort:  54321,
		DestIP:      net.ParseIP("8.8.8.8"),
		DestPort:    443,
		Protocol:    6,
		Direction:   DirectionOutgoing,
		EventType:   EventEstablished,
		Timestamp:   time.Now(),
	}
	sm.ProcessEvent(estEvt)

	// Check connection is established
	stats := sm.GetStats()
	if stats.Established != 1 {
		t.Errorf("GetStats() Established = %d, want 1", stats.Established)
	}
	if stats.PendingOutgoing != 0 {
		t.Errorf("GetStats() PendingOutgoing = %d, want 0", stats.PendingOutgoing)
	}
}

func TestStateMachine_SYNTimeout(t *testing.T) {
	var failedCalled int32

	sm := NewStateMachine(StateMachineConfig{
		SYNTimeout: 50 * time.Millisecond,
		OnEvent: func(conn *Connection, event ConnectionEvent) {
			if event == EventFailed {
				atomic.AddInt32(&failedCalled, 1)
			}
		},
	})
	defer sm.Stop()

	// Process new connection (SYN sent)
	evt := &ConnectionEventRaw{
		SourceIP:    net.ParseIP("192.168.1.100"),
		SourcePort:  54321,
		DestIP:      net.ParseIP("8.8.8.8"),
		DestPort:    443,
		Protocol:    6,
		Direction:   DirectionOutgoing,
		EventType:   EventNew,
		Timestamp:   time.Now(),
	}
	sm.ProcessEvent(evt)

	// Wait for timeout (checkTimeouts runs every 10 seconds, so we need to wait)
	// For testing, we manually trigger the timeout check
	time.Sleep(100 * time.Millisecond)

	// Manually check connections for timeout (simulating checkTimeouts)
	sm.mu.Lock()
	now := time.Now()
	for key, conn := range sm.connections {
		if conn.State == StateSynSent {
			if now.Sub(conn.SynSentTime) > sm.synTimeout {
				conn.State = StateClosed
				conn.ClosedTime = now
				sm.emitEvent(conn, EventFailed)
				delete(sm.connections, key)
			}
		}
	}
	sm.mu.Unlock()

	// Give callback time to execute
	time.Sleep(10 * time.Millisecond)

	// Check failure was detected
	if atomic.LoadInt32(&failedCalled) == 0 {
		t.Error("EventFailed not called on timeout")
	}
}

func TestStateMachine_GetAllConnections(t *testing.T) {
	sm := NewStateMachine(StateMachineConfig{})
	defer sm.Stop()

	// Add multiple connections
	for i := 0; i < 5; i++ {
		evt := &ConnectionEventRaw{
			SourceIP:    net.ParseIP("192.168.1.100"),
			SourcePort:  uint16(54321 + i),
			DestIP:      net.ParseIP("8.8.8.8"),
			DestPort:    443,
			Protocol:    6,
			Direction:   DirectionOutgoing,
			EventType:   EventNew,
			Timestamp:   time.Now(),
		}
		sm.ProcessEvent(evt)
	}

	conns := sm.GetAllConnections()
	if len(conns) != 5 {
		t.Errorf("GetAllConnections() = %d, want 5", len(conns))
	}
}

func TestMakeConnectionKey(t *testing.T) {
	srcIP := net.ParseIP("192.168.1.100")
	dstIP := net.ParseIP("8.8.8.8")

	key1 := makeConnectionKey(srcIP, 54321, dstIP, 443, 6)
	key2 := makeConnectionKey(srcIP, 54321, dstIP, 443, 6)
	key3 := makeConnectionKey(srcIP, 54322, dstIP, 443, 6)

	if key1 != key2 {
		t.Error("Same connection should produce same key")
	}
	if key1 == key3 {
		t.Error("Different port should produce different key")
	}
}
