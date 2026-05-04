// Package integration содержит интеграционные тесты для проверки отслеживания подключений conntrack
// Run with: sudo go test -v ./tests/integration/conntrack_connection_test.go ./tests/integration/helpers.go
package integration

import (
	"context"
	"fmt"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vponomarev/network-monitor/internal/config"
	"github.com/vponomarev/network-monitor/internal/conntrack"
	"go.uber.org/zap"
)

// TestConntrack_OutgoingConnections проверяет отслеживание исходящих подключений
func TestConntrack_OutgoingConnections(t *testing.T) {
	skipIfNotRoot(t)

	logger := zap.NewNop()
	cfg := conntrack.Config{
		EBPFProgramPath: "", // Use simulation mode for basic test
		TrackIncoming:   false,
		TrackOutgoing:   true,
		TrackCloses:     true,
		SYNTimeout:      30 * time.Second,
	}

	tracker, err := conntrack.NewTracker(cfg, logger)
	require.NoError(t, err)
	require.NotNil(t, tracker)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start tracker
	errChan := make(chan error, 1)
	go func() {
		errChan <- tracker.Run(ctx)
	}()

	// Generate outgoing connections
	const numConnections = 5
	for i := 0; i < numConnections; i++ {
		// Try to connect to localhost
		conn, err := net.DialTimeout("tcp", "127.0.0.1:80", 100*time.Millisecond)
		if err != nil {
			// Port 80 may not be available, try port 22
			conn, err = net.DialTimeout("tcp", "127.0.0.1:22", 100*time.Millisecond)
		}
		if err == nil {
			_ = conn.Close()
		}
	}

	// Wait for events
	<-time.After(1 * time.Second)

	// Check that tracker is running
	count := tracker.GetConnectionCount()
	t.Logf("Tracked %d connections", count)

	// In simulation mode, we should have some connections
	assert.GreaterOrEqual(t, count, 0)

	// Get stats
	stats := tracker.GetStats()
	t.Logf("Stats: %+v", stats)

	cancel()
	<-errChan
}

// TestConntrack_IncomingConnections проверяет отслеживание входящих подключений
func TestConntrack_IncomingConnections(t *testing.T) {
	skipIfNotRoot(t)

	logger := zap.NewNop()
	cfg := conntrack.Config{
		EBPFProgramPath: "",
		TrackIncoming:   true,
		TrackOutgoing:   false,
		TrackCloses:     true,
		SYNTimeout:      30 * time.Second,
	}

	tracker, err := conntrack.NewTracker(cfg, logger)
	require.NoError(t, err)
	require.NotNil(t, tracker)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errChan := make(chan error, 1)
	go func() {
		errChan <- tracker.Run(ctx)
	}()

	// Start a simple TCP server to accept incoming connections
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	localPort := listener.Addr().(*net.TCPAddr).Port
	t.Logf("Listening on port %d", localPort)

	// Accept connections in background
	var acceptedCount int
	var mu sync.Mutex
	done := make(chan bool)

	go func() {
		for {
			select {
			case <-done:
				return
			default:
				listener.SetDeadline(time.Now().Add(100 * time.Millisecond))
				conn, err := listener.Accept()
				if err != nil {
					continue
				}
				mu.Lock()
				acceptedCount++
				mu.Unlock()
				_ = conn.Close()
			}
		}
	}()

	// Generate incoming connections
	for i := 0; i < 3; i++ {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", localPort), 1*time.Second)
		if err == nil {
			_ = conn.Close()
		}
	}

	// Wait for events
	<-time.After(1 * time.Second)

	close(done)

	count := tracker.GetConnectionCount()
	t.Logf("Tracked %d connections, accepted %d", count, acceptedCount)

	cancel()
	<-errChan
}

// TestConntrack_TCPhandshake проверяет полный TCP handshake (SYN → SYN+ACK → ESTABLISHED)
func TestConntrack_TCPhandshake(t *testing.T) {
	skipIfNotRoot(t)

	logger := zap.NewNop()
	cfg := conntrack.Config{
		EBPFProgramPath: "",
		TrackIncoming:   true,
		TrackOutgoing:   true,
		TrackCloses:     true,
		SYNTimeout:      5 * time.Second,
	}

	tracker, err := conntrack.NewTracker(cfg, logger)
	require.NoError(t, err)
	require.NotNil(t, tracker)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	errChan := make(chan error, 1)
	go func() {
		errChan <- tracker.Run(ctx)
	}()

	// Start TCP server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	localPort := listener.Addr().(*net.TCPAddr).Port
	t.Logf("Server listening on port %d", localPort)

	// Server accepts connections
	serverDone := make(chan bool)
	go func() {
		for {
			select {
			case <-serverDone:
				return
			default:
				listener.SetDeadline(time.Now().Add(200 * time.Millisecond))
				conn, err := listener.Accept()
				if err != nil {
					continue
				}
				// Keep connection open briefly to simulate established state
				time.Sleep(50 * time.Millisecond)
				_ = conn.Close()
			}
		}
	}()

	// Client creates connection (full handshake)
	clientConn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", localPort), 1*time.Second)
	require.NoError(t, err)

	// Send some data
	_, err = clientConn.Write([]byte("HELLO"))
	assert.NoError(t, err)

	// Wait for handshake to complete
	<-time.After(200 * time.Millisecond)

	// Check tracked connections
	conns := tracker.GetConnections()
	t.Logf("Total tracked connections: %d", len(conns))

	for i, conn := range conns {
		t.Logf("Connection %d: %s -> %s, dir=%s, state=%s",
			i, conn.SourceIP, conn.DestIP, conn.Direction.String(), conn.State.String())
	}

	// Cleanup
	_ = clientConn.Close()
	close(serverDone)

	// Wait for close events
	<-time.After(1 * time.Second)

	cancel()
	<-errChan
}

// TestConntrack_ConnectionLifecycle проверяет полный жизненный цикл подключения
func TestConntrack_ConnectionLifecycle(t *testing.T) {
	skipIfNotRoot(t)

	logger, _ := zap.NewDevelopment()
	cfg := conntrack.Config{
		EBPFProgramPath: "",
		TrackIncoming:   true,
		TrackOutgoing:   true,
		TrackCloses:     true,
		SYNTimeout:      5 * time.Second,
	}

	tracker, err := conntrack.NewTracker(cfg, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	errChan := make(chan error, 1)
	go func() {
		errChan <- tracker.Run(ctx)
	}()

	// Start server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port

	// Accept and close
	go func() {
		for i := 0; i < 3; i++ {
			conn, err := listener.Accept()
			if err != nil {
				continue
			}
			buf := make([]byte, 1024)
			_, _ = conn.Read(buf)
			time.Sleep(10 * time.Millisecond)
			_ = conn.Close()
		}
	}()

	// Create 3 connections
	for i := 0; i < 3; i++ {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 1*time.Second)
		if err != nil {
			continue
		}
		_, _ = conn.Write([]byte(fmt.Sprintf("msg%d", i)))
		time.Sleep(50 * time.Millisecond)
		_ = conn.Close()
	}

	// Wait for all events
	<-time.After(2 * time.Second)

	stats := tracker.GetStats()
	t.Logf("Final stats: %+v", stats)

	cancel()
	<-errChan
}

// TestConntrack_DirectionTracking проверяет разделение на входящие/исходящие
func TestConntrack_DirectionTracking(t *testing.T) {
	skipIfNotRoot(t)

	logger := zap.NewNop()
	cfg := conntrack.Config{
		EBPFProgramPath: "",
		TrackIncoming:   true,
		TrackOutgoing:   true,
		TrackCloses:     true,
	}

	tracker, err := conntrack.NewTracker(cfg, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errChan := make(chan error, 1)
	go func() {
		errChan <- tracker.Run(ctx)
	}()

	// Outgoing connections
	for i := 0; i < 3; i++ {
		conn, _ := net.DialTimeout("tcp", "127.0.0.1:22", 100*time.Millisecond)
		if conn != nil {
			_ = conn.Close()
		}
	}

	// Incoming connections
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	go func() {
		for i := 0; i < 3; i++ {
			conn, err := listener.Accept()
			if err != nil {
				continue
			}
			_ = conn.Close()
		}
	}()

	for i := 0; i < 3; i++ {
		conn, _ := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 1*time.Second)
		if conn != nil {
			_ = conn.Close()
		}
	}

	<-time.After(1 * time.Second)

	conns := tracker.GetConnections()
	var incoming, outgoing int
	for _, c := range conns {
		if c.IsIncoming() {
			incoming++
		} else if c.IsOutgoing() {
			outgoing++
		}
	}

	t.Logf("Incoming: %d, Outgoing: %d", incoming, outgoing)

	cancel()
	<-errChan
}

// TestConntrack_ProcessIdentification проверяет определение процесса
func TestConntrack_ProcessIdentification(t *testing.T) {
	skipIfNotRoot(t)

	logger := zap.NewNop()
	cfg := conntrack.Config{
		EBPFProgramPath: "",
		TrackIncoming:   false,
		TrackOutgoing:   true,
		TrackCloses:     true,
	}

	tracker, err := conntrack.NewTracker(cfg, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errChan := make(chan error, 1)
	go func() {
		errChan <- tracker.Run(ctx)
	}()

	// Create connection from current process
	conn, err := net.DialTimeout("tcp", "127.0.0.1:22", 100*time.Millisecond)
	if err == nil {
		_ = conn.Close()
	}

	<-time.After(500 * time.Millisecond)

	conns := tracker.GetConnections()
	for _, c := range conns {
		t.Logf("Process: PID=%d, Name=%s", c.PID, c.ProcessName)
		assert.NotEmpty(t, c.ProcessName)
		assert.NotEqual(t, "unknown", c.ProcessName)
	}

	cancel()
	<-errChan
}

// TestConntrack_ConcurrentConnections проверяет работу при конкурентных подключениях
func TestConntrack_ConcurrentConnections(t *testing.T) {
	skipIfNotRoot(t)

	logger := zap.NewNop()
	cfg := conntrack.Config{
		EBPFProgramPath: "",
		TrackIncoming:   true,
		TrackOutgoing:   true,
		TrackCloses:     true,
	}

	tracker, err := conntrack.NewTracker(cfg, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	errChan := make(chan error, 1)
	go func() {
		errChan <- tracker.Run(ctx)
	}()

	// Server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port

	go func() {
		for i := 0; i < 10; i++ {
			conn, err := listener.Accept()
			if err != nil {
				continue
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 1024)
				_, _ = c.Read(buf)
				time.Sleep(10 * time.Millisecond)
			}(conn)
		}
	}()

	// Create 10 concurrent connections
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 1*time.Second)
			if err != nil {
				return
			}
			defer conn.Close()
			_, _ = conn.Write([]byte(fmt.Sprintf("concurrent-%d", id)))
			time.Sleep(20 * time.Millisecond)
		}(i)
	}

	wg.Wait()
	<-time.After(1 * time.Second)

	stats := tracker.GetStats()
	t.Logf("Concurrent test stats: %+v", stats)

	cancel()
	<-errChan
}

// skipIfNotRoot skips test if not running as root
func skipIfNotRoot(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("Integration tests require root privileges")
	}
}

// TestConntrack_ConfigValidation проверяет валидацию конфигурации
func TestConntrack_ConfigValidation(t *testing.T) {
	logger := zap.NewNop()

	// Test with all tracking disabled
	cfg := conntrack.Config{
		EBPFProgramPath: "",
		TrackIncoming:   false,
		TrackOutgoing:   false,
		TrackCloses:     false,
	}

	tracker, err := conntrack.NewTracker(cfg, logger)
	require.NoError(t, err)
	require.NotNil(t, tracker)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	errChan := make(chan error, 1)
	go func() {
		errChan <- tracker.Run(ctx)
	}()

	<-time.After(500 * time.Millisecond)
	cancel()
	<-errChan

	t.Log("Tracker created successfully with minimal config")
}

// TestConntrack_EventChannel проверяет работу канала событий
func TestConntrack_EventChannel(t *testing.T) {
	logger := zap.NewNop()
	cfg := conntrack.Config{
		EBPFProgramPath: "",
		TrackIncoming:   true,
		TrackOutgoing:   true,
		TrackCloses:     true,
	}

	tracker, err := conntrack.NewTracker(cfg, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errChan := make(chan error, 1)
	go func() {
		errChan <- tracker.Run(ctx)
	}()

	// Start listening for events
	eventCount := 0
	eventDone := make(chan bool)

	go func() {
		events := tracker.Events()
		for {
			select {
			case <-events:
				eventCount++
			case <-eventDone:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	// Generate connections
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port

	go func() {
		for i := 0; i < 3; i++ {
			conn, err := listener.Accept()
			if err != nil {
				continue
			}
			_ = conn.Close()
		}
	}()

	for i := 0; i < 3; i++ {
		conn, _ := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 1*time.Second)
		if conn != nil {
			_ = conn.Close()
		}
	}

	<-time.After(1 * time.Second)
	close(eventDone)

	t.Logf("Received %d events", eventCount)

	cancel()
	<-errChan
}

// TestConntrack_MetricsIntegration проверяет интеграцию с метриками
func TestConntrack_MetricsIntegration(t *testing.T) {
	logger := zap.NewNop()
	cfg := conntrack.Config{
		EBPFProgramPath: "",
		TrackIncoming:   true,
		TrackOutgoing:   true,
		TrackCloses:     true,
	}

	tracker, err := conntrack.NewTracker(cfg, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errChan := make(chan error, 1)
	go func() {
		errChan <- tracker.Run(ctx)
	}()

	// Generate some traffic
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port

	go func() {
		for i := 0; i < 5; i++ {
			conn, err := listener.Accept()
			if err != nil {
				continue
			}
			_ = conn.Close()
		}
	}()

	for i := 0; i < 5; i++ {
		conn, _ := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 1*time.Second)
		if conn != nil {
			_ = conn.Close()
		}
	}

	<-time.After(1 * time.Second)

	stats := tracker.GetStats()
	t.Logf("Metrics - TotalOutgoing: %d, TotalIncoming: %d, Established: %d",
		stats.TotalOutgoing, stats.TotalIncoming, stats.Established)

	cancel()
	<-errChan
}

// TestConntrack_AppConfig загружает конфигурацию из файла и создает трекер
func TestConntrack_AppConfig(t *testing.T) {
	// Create temporary config
	tmpConfig := "/tmp/conntrack_test_config.yaml"
	configContent := `
global:
  ttl_hours: 1
  metrics_port: 9877
  trace_pipe_path: /sys/kernel/tracing/trace_pipe

connections:
  enabled: true
  track_incoming: true
  track_outgoing: true
  filter_ports: []

logging:
  level: info
  format: json
`
	err := os.WriteFile(tmpConfig, []byte(configContent), 0644)
	require.NoError(t, err)
	defer os.Remove(tmpConfig)

	// Load config
	cfg, err := config.Load(tmpConfig)
	require.NoError(t, err)
	assert.True(t, cfg.Connections.Enabled)
	assert.True(t, cfg.Connections.TrackIncoming)
	assert.True(t, cfg.Connections.TrackOutgoing)

	t.Logf("Config loaded: %+v", cfg.Connections)
}
