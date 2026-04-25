package collector

import (
	"context"
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
	collector := NewTracePipeCollector(TracePipePath, exporter, logger)

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
	collector := NewTracePipeCollector(TracePipePath, exporter, logger)

	// Test non-retransmit line
	line := "          <...>-12345 [001] d.H. 12345.678901: tcp_connect: ..."
	collector.processLine(line)

	assert.Len(t, exporter.events, 0)
}

func TestTracePipeCollector_processLine_NoMatch(t *testing.T) {
	logger := zap.NewNop()
	exporter := &mockExporter{events: make([]TCPRetransmitEvent, 0)}
	collector := NewTracePipeCollector(TracePipePath, exporter, logger)

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
	collector := NewTracePipeCollector("/nonexistent/trace_pipe", exporter, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Should fail quickly due to non-existent path
	err := collector.Run(ctx)
	assert.Error(t, err)
}
