//go:build linux
// +build linux

package conntrack

import (
	"context"
	"encoding/binary"
	"net"
	"testing"
	"time"
	"unsafe"

	"github.com/cilium/ebpf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func newTestTracker(t *testing.T, cfg Config) *Tracker {
	t.Helper()
	tracker, err := NewTracker(cfg, zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(tracker.close)
	return tracker
}

func TestNewTracker(t *testing.T) {
	cfg := Config{
		TrackIncoming: true,
		TrackOutgoing: true,
	}

	tracker := newTestTracker(t, cfg)
	require.NotNil(t, tracker)

	assert.True(t, tracker.config.TrackIncoming)
	assert.True(t, tracker.config.TrackOutgoing)
	assert.Equal(t, DefaultStateTTL, tracker.config.StateTTL)
	assert.Equal(t, DefaultCleanupInterval, tracker.config.CleanupInterval)
	assert.Equal(t, DefaultMaxTrackedConnections, tracker.config.MaxTrackedConnections)
	assert.Equal(t, DefaultMaxPendingConnections, tracker.config.MaxPendingConnections)
	assert.NotNil(t, tracker.stateMachine)
	assert.NotNil(t, tracker.events)
}

func TestConnection_DirectionString(t *testing.T) {
	tests := []struct {
		direction Direction
		expected  string
	}{
		{DirectionIncoming, "incoming"},
		{DirectionOutgoing, "outgoing"},
		{Direction(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.direction.String())
		})
	}
}

func TestTracker_connectionKey(t *testing.T) {
	conn := &Connection{
		SourceIP:   net.ParseIP("192.168.1.1"),
		SourcePort: 12345,
		DestIP:     net.ParseIP("10.0.0.1"),
		DestPort:   443,
		Protocol:   6,
	}

	key := makeConnectionKey(conn.SourceIP, conn.SourcePort, conn.DestIP, conn.DestPort, conn.Protocol)
	assert.Equal(t, "192.168.1.1:12345-10.0.0.1:443-6", key)

	// Same connection should produce same key
	key2 := makeConnectionKey(conn.SourceIP, conn.SourcePort, conn.DestIP, conn.DestPort, conn.Protocol)
	assert.Equal(t, key, key2)
}

func TestTracker_GetConnections(t *testing.T) {
	cfg := Config{}
	tracker := newTestTracker(t, cfg)

	// Initially empty
	conns := tracker.GetConnections()
	assert.Empty(t, conns)
	assert.Equal(t, 0, tracker.GetConnectionCount())

	tracker.stateMachine.ProcessEvent(&ConnectionEventRaw{
		SourceIP: net.ParseIP("1.1.1.1"), SourcePort: 1001,
		DestIP: net.ParseIP("2.2.2.2"), DestPort: 443,
		Protocol: 6, Direction: DirectionOutgoing, EventType: EventEstablished,
	})
	tracker.stateMachine.ProcessEvent(&ConnectionEventRaw{
		SourceIP: net.ParseIP("1.1.1.2"), SourcePort: 1002,
		DestIP: net.ParseIP("2.2.2.2"), DestPort: 443,
		Protocol: 6, Direction: DirectionOutgoing, EventType: EventEstablished,
	})

	conns = tracker.GetConnections()
	assert.Len(t, conns, 2)
	assert.Equal(t, 2, tracker.GetConnectionCount())
}

func TestTracker_Events(t *testing.T) {
	cfg := Config{}
	tracker := newTestTracker(t, cfg)

	events := tracker.Events()
	require.NotNil(t, events)
	assert.True(t, tracker.eventsEnabled.Load())
}

func TestTracker_DoesNotQueueEventsWithoutSubscriber(t *testing.T) {
	tracker := newTestTracker(t, Config{})
	tracker.onConnectionEvent(&Connection{
		ID: "test", SourceIP: net.ParseIP("192.0.2.1"), DestIP: net.ParseIP("198.51.100.1"),
		Protocol: 6, Direction: DirectionOutgoing, State: StateEstablished,
	}, EventEstablished)
	assert.Len(t, tracker.events, 0)
	assert.Equal(t, uint64(0), tracker.GetDroppedEvents())
}

// NOTE: TestTracker_sendEvent and TestTracker_simulateEvents were removed in
// TASK-12 — they targeted a removed conntrack API (Tracker.sendEvent,
// simulateEvents(ctx), and an events.Event-typed channel). The events channel
// now carries *Connection. See docs/prod-readiness/APPENDIX-conntrack-later.md
// (C-8); rewriting conntrack tests belongs to the deferred conntrack track.

func TestTracker_parseConnectionEventRejectsShortRecord(t *testing.T) {
	tracker := newTestTracker(t, Config{})
	assert.Nil(t, tracker.parseConnectionEvent([]byte{0x01, 0x02, 0x03, 0x04}))
}

func TestTracker_Run_ContextCancellation(t *testing.T) {
	cfg := Config{EBPFProgramPath: ""} // Use the embedded production program.

	tracker := newTestTracker(t, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// Run should exit on context cancellation
	err := tracker.Run(ctx)
	assert.NoError(t, err)
}

func Test_sanitizeProcessName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"normal", "sshd", "sshd"},
		{"with null bytes end", "sshd\x00\x00\x00", "sshd"},
		{"empty", "", "unknown"},
		{"only nulls", "\x00\x00\x00\x00", "unknown"},
		{"with spaces", "  nginx  ", "nginx"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, sanitizeProcessName(tt.input))
		})
	}
}

func Test_bpfConnectionEvent_StructAlignment(t *testing.T) {
	// Test that Go struct matches C struct
	// C struct: 8+8+4+4+16+16+2+2+1+1+1+1+1+7(pad)+16 = 88 bytes

	// Check total size
	assert.Equal(t, uintptr(88), unsafe.Sizeof(bpfConnectionEvent{}),
		"bpfConnectionEvent size must be 88 bytes")

	// Check Comm offset (must be 72 after 7-byte padding)
	assert.Equal(t, uintptr(72), unsafe.Offsetof(bpfConnectionEvent{}.Comm),
		"bpfConnectionEvent.Comm must start at offset 72")

	// Check other critical offsets
	assert.Equal(t, uintptr(0), unsafe.Offsetof(bpfConnectionEvent{}.TimestampNs))
	assert.Equal(t, uintptr(8), unsafe.Offsetof(bpfConnectionEvent{}.PidTgid))
	assert.Equal(t, uintptr(16), unsafe.Offsetof(bpfConnectionEvent{}.PID))
	assert.Equal(t, uintptr(24), unsafe.Offsetof(bpfConnectionEvent{}.SrcIP))
	assert.Equal(t, uintptr(40), unsafe.Offsetof(bpfConnectionEvent{}.DstIP))
	assert.Equal(t, uintptr(56), unsafe.Offsetof(bpfConnectionEvent{}.SrcPort))
	assert.Equal(t, uintptr(64), unsafe.Offsetof(bpfConnectionEvent{}.TCPFlags))
}

func Test_validateBpfConnectionEvent(t *testing.T) {
	// This should pass if struct is correctly defined
	err := validateBpfConnectionEvent()
	assert.NoError(t, err)
}

func TestCleanupTimestampedMap(t *testing.T) {
	m, err := ebpf.NewMap(&ebpf.MapSpec{
		Name:       "retention_test",
		Type:       ebpf.Hash,
		KeySize:    8,
		ValueSize:  16,
		MaxEntries: 4,
	})
	if err != nil {
		t.Skipf("creating eBPF map requires kernel privileges: %v", err)
	}
	defer m.Close()

	now := uint64(10 * time.Hour)
	oldValue := make([]byte, 16)
	freshValue := make([]byte, 16)
	binary.LittleEndian.PutUint64(oldValue, now-uint64(2*time.Hour))
	binary.LittleEndian.PutUint64(freshValue, now-uint64(30*time.Minute))
	require.NoError(t, m.Put(uint64(1), oldValue))
	require.NoError(t, m.Put(uint64(2), freshValue))

	remaining, deleted, err := cleanupTimestampedMap(m, now, time.Hour)
	require.NoError(t, err)
	assert.Equal(t, 1, deleted)
	assert.Equal(t, 1, remaining)

	var value [16]byte
	assert.ErrorIs(t, m.Lookup(uint64(1), &value), ebpf.ErrKeyNotExist)
	require.NoError(t, m.Lookup(uint64(2), &value))
}
