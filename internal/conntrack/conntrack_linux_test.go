//go:build linux
// +build linux

package conntrack

import (
	"context"
	"net"
	"testing"
	"time"
	"unsafe"

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

	assert.Equal(t, cfg, tracker.config)
	assert.NotNil(t, tracker.connections)
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

	// Add connections manually
	tracker.mu.Lock()
	tracker.connections["key1"] = &Connection{SourceIP: net.ParseIP("1.1.1.1")}
	tracker.connections["key2"] = &Connection{SourceIP: net.ParseIP("2.2.2.2")}
	tracker.mu.Unlock()

	conns = tracker.GetConnections()
	assert.Len(t, conns, 2)
	assert.Equal(t, 2, tracker.GetConnectionCount())
}

func TestTracker_Events(t *testing.T) {
	cfg := Config{}
	tracker := newTestTracker(t, cfg)

	events := tracker.Events()
	require.NotNil(t, events)
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
