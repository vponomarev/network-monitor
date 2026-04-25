//go:build linux
// +build linux

package conntrack

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vponomarev/network-monitor/pkg/events"
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
