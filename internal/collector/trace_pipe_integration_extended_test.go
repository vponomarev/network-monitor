//go:build integration
// +build integration

package collector

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestTracePipeCollector_Integration_RealTraffic tests with real trace_pipe
func TestTracePipeCollector_Integration_RealTraffic(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("Integration test requires root privileges")
	}

	if _, err := os.Stat(TracePipePath); os.IsNotExist(err) {
		t.Skipf("trace_pipe not available at %s", TracePipePath)
	}

	logger := zap.NewNop()
	exporter := &mockExporterForIntegration{events: make([]TCPRetransmitEvent, 0)}
	collector := NewTracePipeCollector(TracePipePath, exporter, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Start collector
	done := make(chan error, 1)
	go func() {
		done <- collector.Run(ctx)
	}()

	// Generate some network traffic
	go generateNetworkTraffic()

	// Wait for collector
	select {
	case err := <-done:
		t.Logf("Collector finished: %v", err)
	case <-time.After(4 * time.Second):
		cancel()
	}

	t.Logf("Captured %d retransmit events", exporter.Count())
}

// TestTracePipeCollector_Integration_HighLoad tests collector under high load
func TestTracePipeCollector_Integration_HighLoad(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("Integration test requires root privileges")
	}

	if _, err := os.Stat(TracePipePath); os.IsNotExist(err) {
		t.Skipf("trace_pipe not available at %s", TracePipePath)
	}

	logger := zap.NewNop()
	exporter := &mockExporterForIntegration{events: make([]TCPRetransmitEvent, 0)}
	collector := NewTracePipeCollector(TracePipePath, exporter, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- collector.Run(ctx)
	}()

	// Generate high load traffic
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			generateNetworkTraffic()
		}(i)
	}

	wg.Wait()

	select {
	case err := <-done:
		t.Logf("Collector finished: %v", err)
	case <-time.After(6 * time.Second):
		cancel()
	}

	t.Logf("High load test - Captured %d retransmit events", exporter.Count())
}

// TestTracePipeCollector_Integration_LongRunning tests long-running collection
func TestTracePipeCollector_Integration_LongRunning(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("Integration test requires root privileges")
	}

	if _, err := os.Stat(TracePipePath); os.IsNotExist(err) {
		t.Skipf("trace_pipe not available at %s", TracePipePath)
	}

	logger := zap.NewNop()
	exporter := &mockExporterForIntegration{events: make([]TCPRetransmitEvent, 0)}
	collector := NewTracePipeCollector(TracePipePath, exporter, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- collector.Run(ctx)
	}()

	// Periodic traffic generation
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	trafficDone := make(chan bool)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				generateNetworkTraffic()
			}
		}
	}()

	select {
	case err := <-done:
		t.Logf("Collector finished: %v", err)
	case <-trafficDone:
		cancel()
	case <-time.After(11 * time.Second):
		cancel()
	}

	t.Logf("Long running test - Captured %d retransmit events", exporter.Count())
}

// TestTracePipeCollector_Integration_MultipleCollectors tests multiple collectors
func TestTracePipeCollector_Integration_MultipleCollectors(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("Integration test requires root privileges")
	}

	if _, err := os.Stat(TracePipePath); os.IsNotExist(err) {
		t.Skipf("trace_pipe not available at %s", TracePipePath)
	}

	logger := zap.NewNop()

	// Create multiple collectors
	collectors := make([]*TracePipeCollector, 3)
	exporters := make([]*mockExporterForIntegration, 3)

	for i := 0; i < 3; i++ {
		exporters[i] = &mockExporterForIntegration{events: make([]TCPRetransmitEvent, 0)}
		collectors[i] = NewTracePipeCollector(TracePipePath, exporters[i], logger)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Start all collectors
	done := make(chan error, 3)
	for i, collector := range collectors {
		go func(id int, c *TracePipeCollector) {
			done <- c.Run(ctx)
		}(i, collector)
	}

	// Generate traffic
	generateNetworkTraffic()

	// Wait for all
	for i := 0; i < 3; i++ {
		select {
		case err := <-done:
			t.Logf("Collector %d finished: %v", i, err)
		case <-time.After(3 * time.Second):
			t.Errorf("Collector %d timed out", i)
		}
	}

	for i, exporter := range exporters {
		t.Logf("Collector %d captured %d events", i, exporter.Count())
	}
}

// TestTracePipeCollector_Integration_MemoryUsage tests memory usage over time
func TestTracePipeCollector_Integration_MemoryUsage(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("Integration test requires root privileges")
	}

	if _, err := os.Stat(TracePipePath); os.IsNotExist(err) {
		t.Skipf("trace_pipe not available at %s", TracePipePath)
	}

	logger := zap.NewNop()
	exporter := &mockExporterForIntegration{events: make([]TCPRetransmitEvent, 0)}
	collector := NewTracePipeCollector(TracePipePath, exporter, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- collector.Run(ctx)
	}()

	// Continuous traffic
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	startMem := getMemoryUsage()

	for i := 0; i < 50; i++ {
		select {
		case <-ticker.C:
			generateNetworkTraffic()
		case <-ctx.Done():
			break
		}
	}

	endMem := getMemoryUsage()
	memGrowth := endMem - startMem

	t.Logf("Memory usage: start=%d KB, end=%d KB, growth=%d KB", startMem, endMem, memGrowth)
	t.Logf("Captured %d events", exporter.Count())

	// Memory growth should be reasonable (less than 10MB)
	if memGrowth > 10000 {
		t.Errorf("Excessive memory growth: %d KB", memGrowth)
	}
}

// Helper functions

func generateNetworkTraffic() {
	// Try to connect to various addresses to generate traffic
	addresses := []string{
		"127.0.0.1:80",
		"127.0.0.1:443",
		"127.0.0.1:22",
	}

	for _, addr := range addresses {
		conn, err := os.OpenFile("/dev/tcp/"+addr, os.O_RDWR, 0666)
		if err == nil {
			conn.Close()
		}
	}
}

func getMemoryUsage() int64 {
	// Read memory usage from /proc/self/status
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}

	for _, line := range string(data) {
		if line == '\n' {
			break
		}
	}

	// Simple parsing for VmRSS
	lines := string(data)
	for _, l := range splitLines(lines) {
		if len(l) > 6 && l[:6] == "VmRSS:" {
			var mem int64
			fmt.Sscanf(l, "VmRSS: %d", &mem)
			return mem
		}
	}
	return 0
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i, c := range s {
		if c == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
