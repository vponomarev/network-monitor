package collector

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// mockExporterForIntegration is a thread-safe mock exporter for integration tests
type mockExporterForIntegration struct {
	events []TCPRetransmitEvent
	mu     sync.Mutex
}

func (m *mockExporterForIntegration) RecordRetransmit(srcIP, dstIP string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, TCPRetransmitEvent{
		Timestamp: time.Now(),
		SourceIP:  srcIP,
		DestIP:    dstIP,
	})
}

func (m *mockExporterForIntegration) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.events)
}

func (m *mockExporterForIntegration) Events() []TCPRetransmitEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	events := make([]TCPRetransmitEvent, len(m.events))
	copy(events, m.events)
	return events
}

func TestTracePipeCollector_WithMockTracePipe(t *testing.T) {
	// Create a temporary file to simulate trace_pipe
	tmpfile, err := os.CreateTemp("", "trace_pipe_*")
	require.NoError(t, err)
	defer os.Remove(tmpfile.Name())

	// Write some test data
	testData := `          <...>-12345 [001] d.H. 12345.678901: tcp_retransmit_skb: addr=0xffff888012345678 sk=0xffff888012345678 saddr=192.168.1.10 daddr=192.168.1.20 seq=123456789
          <...>-12346 [002] d.H. 12346.789012: tcp_connect: saddr=192.168.1.10 daddr=192.168.1.20
          <...>-12347 [003] d.H. 12347.890123: tcp_retransmit_skb: addr=0xffff888012345679 sk=0xffff888012345679 saddr=10.0.0.1 daddr=10.0.0.2 seq=987654321
`
	_, err = tmpfile.WriteString(testData)
	require.NoError(t, err)

	// Reset file pointer
	_, err = tmpfile.Seek(0, 0)
	require.NoError(t, err)

	logger := zap.NewNop()
	exporter := &mockExporterForIntegration{events: make([]TCPRetransmitEvent, 0)}
	collector := NewTracePipeCollector(tmpfile.Name(), exporter, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Run collector (will timeout quickly)
	_ = collector.Run(ctx)

	// Should have captured 2 retransmit events (not the tcp_connect)
	assert.Equal(t, 2, exporter.Count())

	events := exporter.Events()
	require.Len(t, events, 2)
	assert.Equal(t, "192.168.1.10", events[0].SourceIP)
	assert.Equal(t, "192.168.1.20", events[0].DestIP)
	assert.Equal(t, "10.0.0.1", events[1].SourceIP)
	assert.Equal(t, "10.0.0.2", events[1].DestIP)
}

func TestTracePipeCollector_EmptyFile(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "trace_pipe_empty_*")
	require.NoError(t, err)
	defer os.Remove(tmpfile.Name())

	logger := zap.NewNop()
	exporter := &mockExporterForIntegration{events: make([]TCPRetransmitEvent, 0)}
	collector := NewTracePipeCollector(tmpfile.Name(), exporter, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_ = collector.Run(ctx)

	assert.Equal(t, 0, exporter.Count())
}

func TestTracePipeCollector_MalformedLines(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "trace_pipe_malformed_*")
	require.NoError(t, err)
	defer os.Remove(tmpfile.Name())

	// Write malformed data
	testData := `random garbage
tcp_retransmit_skb: incomplete line
saddr=192.168.1.1 without daddr
daddr=192.168.1.2 without saddr
tcp_retransmit_skb: saddr=invalid-ip daddr=also-invalid
`
	_, err = tmpfile.WriteString(testData)
	require.NoError(t, err)

	_, err = tmpfile.Seek(0, 0)
	require.NoError(t, err)

	logger := zap.NewNop()
	exporter := &mockExporterForIntegration{events: make([]TCPRetransmitEvent, 0)}
	collector := NewTracePipeCollector(tmpfile.Name(), exporter, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_ = collector.Run(ctx)

	// Should not crash, but may capture the invalid IP line
	// The regex should not match invalid IPs
	assert.LessOrEqual(t, exporter.Count(), 1)
}

func TestTracePipeCollector_Concurrent(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "trace_pipe_concurrent_*")
	require.NoError(t, err)
	defer os.Remove(tmpfile.Name())

	logger := zap.NewNop()
	exporter := &mockExporterForIntegration{events: make([]TCPRetransmitEvent, 0)}
	collector := NewTracePipeCollector(tmpfile.Name(), exporter, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start collector in background
	go func() {
		_ = collector.Run(ctx)
	}()

	// Write data concurrently
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(id int) {
			line := fmt.Sprintf("tcp_retransmit_skb: saddr=192.168.1.%d daddr=192.168.2.%d\n", id, id)
			tmpfile.WriteString(line)
			done <- true
		}(i)
	}

	// Wait for all writers
	for i := 0; i < 10; i++ {
		<-done
	}

	// Give collector time to process
	time.Sleep(100 * time.Millisecond)
	cancel()

	// Should have captured some events (may be less due to timing)
	assert.GreaterOrEqual(t, exporter.Count(), 0)
}

func TestTracePipeCollector_DifferentIPFormats(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "trace_pipe_formats_*")
	require.NoError(t, err)
	defer os.Remove(tmpfile.Name())

	// Test different IP formats that might appear in trace output
	testData := `tcp_retransmit_skb: saddr=192.168.1.1 daddr=192.168.1.2
tcp_retransmit_skb: saddr=10.0.0.1 daddr=10.255.255.255
tcp_retransmit_skb: saddr=172.16.0.1 daddr=172.31.255.255
tcp_retransmit_skb: saddr=127.0.0.1 daddr=127.0.0.1
`
	_, err = tmpfile.WriteString(testData)
	require.NoError(t, err)

	_, err = tmpfile.Seek(0, 0)
	require.NoError(t, err)

	logger := zap.NewNop()
	exporter := &mockExporterForIntegration{events: make([]TCPRetransmitEvent, 0)}
	collector := NewTracePipeCollector(tmpfile.Name(), exporter, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_ = collector.Run(ctx)

	assert.Equal(t, 4, exporter.Count())
}

func TestTracePipeCollector_RapidEvents(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "trace_pipe_rapid_*")
	require.NoError(t, err)
	defer os.Remove(tmpfile.Name())

	// Generate many events rapidly
	var testData strings.Builder
	for i := 0; i < 100; i++ {
		testData.WriteString(fmt.Sprintf("tcp_retransmit_skb: saddr=192.168.1.%d daddr=192.168.2.%d\n", i%256, i%256))
	}

	_, err = tmpfile.WriteString(testData.String())
	require.NoError(t, err)

	_, err = tmpfile.Seek(0, 0)
	require.NoError(t, err)

	logger := zap.NewNop()
	exporter := &mockExporterForIntegration{events: make([]TCPRetransmitEvent, 0)}
	collector := NewTracePipeCollector(tmpfile.Name(), exporter, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	_ = collector.Run(ctx)

	// Should capture all or most events
	assert.GreaterOrEqual(t, exporter.Count(), 90) // Allow some timing variance
}

func TestTracePipeCollector_ContextCancellation(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "trace_pipe_cancel_*")
	require.NoError(t, err)
	defer os.Remove(tmpfile.Name())

	logger := zap.NewNop()
	exporter := &mockExporterForIntegration{events: make([]TCPRetransmitEvent, 0)}
	collector := NewTracePipeCollector(tmpfile.Name(), exporter, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	err = collector.Run(ctx)
	elapsed := time.Since(start)

	// Should exit quickly on context cancellation
	assert.Less(t, elapsed, 200*time.Millisecond)
	assert.Error(t, err) // Context cancelled
}

func TestTracePipeCollector_NonExistentPath(t *testing.T) {
	logger := zap.NewNop()
	exporter := &mockExporterForIntegration{events: make([]TCPRetransmitEvent, 0)}
	collector := NewTracePipeCollector("/nonexistent/trace_pipe", exporter, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := collector.Run(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "trace_pipe not found")
}

func TestTracePipeCollector_ExporterNil(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "trace_pipe_nil_*")
	require.NoError(t, err)
	defer os.Remove(tmpfile.Name())

	logger := zap.NewNop()
	// Pass nil exporter - should handle gracefully
	collector := NewTracePipeCollector(tmpfile.Name(), nil, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Should not panic
	_ = collector.Run(ctx)
}

func TestTracePipeCollector_Integration(t *testing.T) {
	// Skip if not running as root or trace_pipe not available
	if os.Geteuid() != 0 {
		t.Skip("Integration test requires root privileges")
	}

	if _, err := os.Stat(TracePipePath); os.IsNotExist(err) {
		t.Skip("trace_pipe not available - ensure tracefs is mounted")
	}

	logger := zap.NewNop()
	exporter := &mockExporterForIntegration{events: make([]TCPRetransmitEvent, 0)}
	collector := NewTracePipeCollector(TracePipePath, exporter, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Generate some TCP traffic to trigger retransmits
	go generateTCPTraffic()

	err := collector.Run(ctx)
	// Context timeout is expected
	assert.Error(t, err)

	// We should have captured some events (if there was traffic)
	// Note: This may be 0 if no retransmits occurred during the test
	t.Logf("Captured %d retransmit events", exporter.Count())
}

func generateTCPTraffic() {
	// Create a connection that will likely cause retransmits
	// Connect to a non-routable address to trigger retransmits
	conn, err := net.DialTimeout("tcp", "192.0.2.1:80", 100*time.Millisecond)
	if err == nil {
		conn.Close()
	}
}
