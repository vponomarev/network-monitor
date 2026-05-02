//go:build linux
// +build linux

package conntrack

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewTracker(t *testing.T) {
	logger := zap.NewNop()
	cfg := Config{
		TrackIncoming: true,
		TrackOutgoing: true,
	}

	tracker, err := NewTracker(cfg, logger)
	require.NoError(t, err)
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
	logger := zap.NewNop()
	cfg := Config{}
	tracker, _ := NewTracker(cfg, logger)

	conn := &Connection{
		SourceIP:   net.ParseIP("192.168.1.1"),
		SourcePort: 12345,
		DestIP:     net.ParseIP("10.0.0.1"),
		DestPort:   443,
		Protocol:   6,
	}

	key := tracker.connectionKey(conn)
	assert.Equal(t, "192.168.1.1:12345-10.0.0.1:443-6", key)

	// Same connection should produce same key
	key2 := tracker.connectionKey(conn)
	assert.Equal(t, key, key2)
}

func TestTracker_GetConnections(t *testing.T) {
	logger := zap.NewNop()
	cfg := Config{}
	tracker, _ := NewTracker(cfg, logger)

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
	logger := zap.NewNop()
	cfg := Config{}
	tracker, _ := NewTracker(cfg, logger)

	events := tracker.Events()
	require.NotNil(t, events)
}

func TestTracker_sendEvent(t *testing.T) {
	logger := zap.NewNop()
	cfg := Config{}
	tracker, _ := NewTracker(cfg, logger)

	conn := &Connection{
		Timestamp:   time.Now(),
		SourceIP:    net.ParseIP("192.168.1.100"),
		SourcePort:  54321,
		DestIP:      net.ParseIP("8.8.8.8"),
		DestPort:    443,
		Protocol:    6,
		Direction:   DirectionOutgoing,
		PID:         1234,
		ProcessName: "test",
	}

	tracker.sendEvent(conn)

	// Check connection was stored
	assert.Equal(t, 1, tracker.GetConnectionCount())

	// Check event was sent
	select {
	case event := <-tracker.Events():
		assert.Equal(t, EventTypeNewConnection, event.Type)
		assert.Equal(t, "conntrack", event.Source)
		data, ok := event.Data.(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "192.168.1.100", data["source_ip"])
		assert.Equal(t, uint16(443), data["dest_port"])
		assert.Equal(t, "outgoing", data["direction"])
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Expected event not received")
	}
}

func TestTracker_parseConnectionEvent(t *testing.T) {
	logger := zap.NewNop()
	cfg := Config{}
	tracker, _ := NewTracker(cfg, logger)

	// Test with sample data
	data := []byte{0x01, 0x02, 0x03, 0x04} // Placeholder
	conn := tracker.parseConnectionEvent(data)

	require.NotNil(t, conn)
	assert.NotNil(t, conn.DestIP)
	assert.Equal(t, uint8(6), conn.Protocol)
}

func TestTracker_Run_ContextCancellation(t *testing.T) {
	logger := zap.NewNop()
	cfg := Config{
		EBPFProgramPath: "", // Use simulation mode
	}

	tracker, err := NewTracker(cfg, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// Run should exit on context cancellation
	err = tracker.Run(ctx)
	assert.NoError(t, err)
}

func TestTracker_simulateEvents(t *testing.T) {
	logger := zap.NewNop()
	cfg := Config{}
	tracker, _ := NewTracker(cfg, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start simulation in background
	go tracker.simulateEvents(ctx)

	// Wait for event
	select {
	case event := <-tracker.Events():
		assert.Equal(t, EventTypeNewConnection, event.Type)
		data, ok := event.Data.(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "outgoing", data["direction"])
	case <-time.After(6 * time.Second):
		t.Fatal("Expected simulated event not received")
	}
}

func Test_sanitizeProcessName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"normal", "sshd", "sshd"},
		{"with null bytes end", "sshd\x00\x00\x00", "sshd"},
		{"with null bytes start", "\x00\x00\x00sshd", "sshd"},
		{"with null bytes both", "\x00\x00sshd\x00\x00", "sshd"},
		{"empty", "", "unknown"},
		{"only nulls", "\x00\x00\x00\x00", "unknown"},
		{"with spaces", "  nginx  ", "nginx"},
		{"nulls then name", "\x00\x00\x00\x00\x00\x00\x00sshd", "sshd"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, sanitizeProcessName(tt.input))
		})
	}
}

func Test_getProcessComm(t *testing.T) {
	// Test with current process PID (should always exist)
	pid := uint32(os.Getpid())
	comm := getProcessComm(pid)
	assert.NotEmpty(t, comm, "Should get comm for current process")
	assert.NotEqual(t, "unknown", comm)
}

func Test_getProcessComm_invalid(t *testing.T) {
	// Test with PID 0 (should return empty)
	comm := getProcessComm(0)
	assert.Empty(t, comm)

	// Test with non-existent PID (should return empty)
	comm = getProcessComm(99999999)
	assert.Empty(t, comm)
}

func Test_enrichProcessName(t *testing.T) {
	// Valid eBPF comm should be returned as-is
	name := enrichProcessName("sshd", 1234)
	assert.Equal(t, "sshd", name)

	// Empty eBPF comm should trigger /proc lookup
	pid := uint32(os.Getpid())
	name = enrichProcessName("\x00\x00\x00\x00\x00\x00\x00\x00", pid)
	assert.NotEmpty(t, name)
	assert.NotEqual(t, "unknown", name)

	// Invalid PID with empty comm should return "unknown"
	name = enrichProcessName("\x00\x00\x00\x00\x00\x00\x00\x00", 99999999)
	assert.Equal(t, "unknown", name)
}
