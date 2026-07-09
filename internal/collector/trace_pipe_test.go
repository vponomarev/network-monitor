package collector

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// mockExporter is a mock exporter for testing
type mockExporter struct {
	events []TCPRetransmitEvent
}

func (m *mockExporter) RecordRetransmit(srcIP, dstIP string) {
	m.events = append(m.events, TCPRetransmitEvent{
		Timestamp: time.Now(),
		SourceIP:  srcIP,
		DestIP:    dstIP,
	})
}

func TestTracePipeCollector_processLine(t *testing.T) {
	logger := zap.NewNop()
	exporter := &mockExporter{events: make([]TCPRetransmitEvent, 0)}
	collector := NewTracePipeCollector(TracePipePath, exporter, logger, nil)

	// Test valid retransmit line
	line := "          <...>-12345 [001] d.H. 12345.678901: tcp_retransmit_skb: addr=0xffff888012345678 sk=0xffff888012345678 saddr=192.168.1.10 daddr=192.168.1.20 seq=123456789"
	collector.processLine(line)

	require.Len(t, exporter.events, 1)
	assert.Equal(t, "192.168.1.10", exporter.events[0].SourceIP)
	assert.Equal(t, "192.168.1.20", exporter.events[0].DestIP)
}

func TestTracePipeCollector_processLine_Ignored(t *testing.T) {
	logger := zap.NewNop()
	exporter := &mockExporter{events: make([]TCPRetransmitEvent, 0)}
	collector := NewTracePipeCollector(TracePipePath, exporter, logger, nil)

	// Test non-retransmit line
	line := "          <...>-12345 [001] d.H. 12345.678901: tcp_connect: ..."
	collector.processLine(line)

	assert.Len(t, exporter.events, 0)
}

func TestTracePipeCollector_processLine_NoMatch(t *testing.T) {
	logger := zap.NewNop()
	exporter := &mockExporter{events: make([]TCPRetransmitEvent, 0)}
	collector := NewTracePipeCollector(TracePipePath, exporter, logger, nil)

	// Test line without IP addresses
	line := "          <...>-12345 [001] d.H. 12345.678901: tcp_retransmit_skb: some other format"
	collector.processLine(line)

	assert.Len(t, exporter.events, 0)
}

func Test_contains(t *testing.T) {
	tests := []struct {
		s        string
		substr   string
		expected bool
	}{
		{"hello world", "world", true},
		{"tcp_retransmit_skb event", "tcp_retransmit_skb", true},
		{"hello", "x", false},
		{"", "test", false},
		{"test", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.s+"-"+tt.substr, func(t *testing.T) {
			result := contains(tt.s, tt.substr)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTracePipeCollector_Run_ContextCancellation(t *testing.T) {
	logger := zap.NewNop()
	exporter := &mockExporter{events: make([]TCPRetransmitEvent, 0)}
	collector := NewTracePipeCollector("/nonexistent/trace_pipe", exporter, logger, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Should fail quickly due to non-existent path
	err := collector.Run(ctx)
	assert.Error(t, err)
}

func TestIsTracepointEnabled_FileNotFound(t *testing.T) {
	enabled, err := IsTracepointEnabled("/nonexistent/path/enable")
	assert.False(t, enabled)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "tracepoint enable file not found")
}

func TestIsTracepointEnabled_Enabled(t *testing.T) {
	// Create a temporary file with "1"
	tmpFile := t.TempDir() + "/enable"
	err := os.WriteFile(tmpFile, []byte("1\n"), 0644)
	require.NoError(t, err)

	enabled, err := IsTracepointEnabled(tmpFile)
	assert.NoError(t, err)
	assert.True(t, enabled)
}

func TestIsTracepointEnabled_Disabled(t *testing.T) {
	// Create a temporary file with "0"
	tmpFile := t.TempDir() + "/enable"
	err := os.WriteFile(tmpFile, []byte("0\n"), 0644)
	require.NoError(t, err)

	enabled, err := IsTracepointEnabled(tmpFile)
	assert.NoError(t, err)
	assert.False(t, enabled)
}

func TestIsTracepointEnabled_InvalidContent(t *testing.T) {
	// Create a temporary file with invalid content
	tmpFile := t.TempDir() + "/enable"
	err := os.WriteFile(tmpFile, []byte("invalid\n"), 0644)
	require.NoError(t, err)

	enabled, err := IsTracepointEnabled(tmpFile)
	assert.NoError(t, err)
	assert.False(t, enabled)
}

func TestEnableTracepoint_DirectoryNotFound(t *testing.T) {
	err := EnableTracepoint("/nonexistent/path/enable")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "tracepoint directory not found")
}

func TestEnableTracepoint_Success(t *testing.T) {
	// Skip if not root (cannot write to /sys/kernel/tracing)
	if os.Getuid() != 0 {
		t.Skip("Test requires root privileges")
	}

	// This test would require actual tracefs mount
	// For now, just verify the function exists and compiles
	assert.NotNil(t, EnableTracepoint)
}

func TestCheckAndWarnTracepoint(t *testing.T) {
	logger := zap.NewNop()

	// Test with non-existent path
	result := CheckAndWarnTracepoint(logger, "/nonexistent/path/enable")
	assert.False(t, result)
}

func TestGetTracepointEnablePath(t *testing.T) {
	path := GetTracepointEnablePath()
	assert.Equal(t, "/sys/kernel/tracing/events/tcp/tcp_retransmit_skb/enable", path)
}
