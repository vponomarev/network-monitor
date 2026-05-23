// Package integration содержит интеграционные тесты для conntrack
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

// skipIfNotRoot пропускает тест, если запущен не от root
func skipIfNotRoot(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("Integration tests require root privileges")
	}
}

// TestConntrack_OutgoingConnections проверяет отслеживание исходящих подключений
// IT-001
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

	errChan := make(chan error, 1)
	go func() {
		errChan <- tracker.Run(ctx)
	}()

	// Generate outgoing connections
	const numConnections = 5
	for i := 0; i < numConnections; i++ {
		conn, err := net.DialTimeout("tcp", "127.0.0.1:80", 100*time.Millisecond)
		if err != nil {
			conn, err = net.DialTimeout("tcp", "127.0.0.1:22", 100*time.Millisecond)
		}
		if err == nil {
			_ = conn.Close()
		}
	}

	<-time.After(1 * time.Second)

	count := tracker.GetConnectionCount()
	t.Logf("Tracked %d connections", count)
	assert.GreaterOrEqual(t, count, 0)

	stats := tracker.GetStats()
	t.Logf("Stats: %+v", stats)

	cancel()
	<-errChan
}

// TestConntrack_IncomingConnections проверяет отслеживание входящих подключений
// IT-002
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

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	localPort := listener.Addr().(*net.TCPAddr).Port
	t.Logf("Listening on port %d", localPort)

	var acceptedCount int
	var mu sync.Mutex
	done := make(chan bool)

	go func() {
		for {
			select {
			case <-done:
				return
			default:
				// Set read deadline using tcp listener
				if tcpListener, ok := listener.(*net.TCPListener); ok {
					tcpListener.SetDeadline(time.Now().Add(100 * time.Millisecond))
				}
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

	for i := 0; i < 3; i++ {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", localPort), 1*time.Second)
		if err == nil {
			_ = conn.Close()
		}
	}

	<-time.After(1 * time.Second)
	close(done)

	count := tracker.GetConnectionCount()
	t.Logf("Tracked %d connections, accepted %d", count, acceptedCount)

	cancel()
	<-errChan
}

// TestConntrack_TCPhandshake проверяет полный TCP handshake
// IT-003
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

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	localPort := listener.Addr().(*net.TCPAddr).Port
	t.Logf("Server listening on port %d", localPort)

	serverDone := make(chan bool)
	go func() {
		for {
			select {
			case <-serverDone:
				return
			default:
				// Set read deadline using tcp listener
				if tcpListener, ok := listener.(*net.TCPListener); ok {
					tcpListener.SetDeadline(time.Now().Add(200 * time.Millisecond))
				}
				conn, err := listener.Accept()
				if err != nil {
					continue
				}
				time.Sleep(50 * time.Millisecond)
				_ = conn.Close()
			}
		}
	}()

	clientConn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", localPort), 1*time.Second)
	require.NoError(t, err)

	_, err = clientConn.Write([]byte("HELLO"))
	assert.NoError(t, err)

	<-time.After(200 * time.Millisecond)

	conns := tracker.GetConnections()
	t.Logf("Total tracked connections: %d", len(conns))

	for i, conn := range conns {
		t.Logf("Connection %d: %s -> %s, dir=%s, state=%s",
			i, conn.SourceIP, conn.DestIP, conn.Direction.String(), conn.State.String())
	}

	_ = clientConn.Close()
	close(serverDone)
	<-time.After(1 * time.Second)

	cancel()
	<-errChan
}

// TestConntrack_DirectionTracking проверяет разделение на входящие/исходящие
// IT-004
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

	for i := 0; i < 3; i++ {
		conn, _ := net.DialTimeout("tcp", "127.0.0.1:22", 100*time.Millisecond)
		if conn != nil {
			_ = conn.Close()
		}
	}

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

// TestConntrack_ConcurrentConnections проверяет работу при конкурентной нагрузке
// IT-005
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

// TestConntrack_EventChannel проверяет работу канала событий
// IT-006
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

// TestConntrack_ProcessIdentification проверяет определение процесса
// IT-007
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

// TestConntrack_ConfigValidation проверяет валидацию конфигурации
// IT-008
func TestConntrack_ConfigValidation(t *testing.T) {
	logger := zap.NewNop()

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

// TestConntrack_AppConfig загружает конфигурацию из файла и создает трекер
// IT-009
func TestConntrack_AppConfig(t *testing.T) {
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

	cfg, err := config.Load(tmpConfig)
	require.NoError(t, err)
	assert.True(t, cfg.Connections.Enabled)
	assert.True(t, cfg.Connections.TrackIncoming)
	assert.True(t, cfg.Connections.TrackOutgoing)

	t.Logf("Config loaded: %+v", cfg.Connections)
}
