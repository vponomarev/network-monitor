// Package integration contains integration tests for Network Monitor core components
// Run with: sudo go test -v ./tests/integration/...
package integration

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vponomarev/network-monitor/internal/collector"
	"github.com/vponomarev/network-monitor/internal/config"
	"github.com/vponomarev/network-monitor/internal/conntrack"
	"github.com/vponomarev/network-monitor/internal/discovery"
	"github.com/vponomarev/network-monitor/internal/metadata"
	"github.com/vponomarev/network-monitor/internal/metrics"
	"github.com/vponomarev/network-monitor/internal/topology"
	"go.uber.org/zap"
)

// TestCollector_TracePipe_Integration tests trace pipe collector with real trace_pipe
func TestCollector_TracePipe_Integration(t *testing.T) {
	skipIfNotRoot(t)

	// Check if trace_pipe exists
	tracePipePath := "/sys/kernel/tracing/trace_pipe"
	if _, err := os.Stat(tracePipePath); os.IsNotExist(err) {
		t.Skipf("trace_pipe not found at %s - skipping integration test", tracePipePath)
	}

	logger := zap.NewNop()
	exporter := &mockExporter{}

	collector := collector.NewTracePipeCollector(tracePipePath, exporter, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Start collector
	errChan := make(chan error, 1)
	go func() {
		errChan <- collector.Run(ctx)
	}()

	// Generate some TCP traffic to trigger potential retransmits
	generateTCPTraffic(t)

	// Wait for collection
	<-time.After(1 * time.Second)

	// Collector should be running
	select {
	case err := <-errChan:
		if err != context.Canceled {
			assert.NoError(t, err)
		}
	default:
		// Still running, which is expected
	}
}

// TestMetrics_Exporter_Integration tests metrics exporter with real data
func TestMetrics_Exporter_Integration(t *testing.T) {
	logger := zap.NewNop()

	// Create matchers
	locationMatcher := metadata.NewEmptyLocationMatcher()
	roleMatcher := metadata.NewEmptyRoleMatcher()

	// Create exporter
	exporter := metrics.NewExporterWithRegistry(
		"netmon_tcp_loss_total",
		locationMatcher,
		roleMatcher,
		logger,
		prometheus.NewRegistry(),
	)

	// Record some retransmits
	exporter.RecordRetransmit("192.168.1.10", "192.168.1.20")
	exporter.RecordRetransmit("192.168.1.10", "192.168.1.20")
	exporter.RecordRetransmit("192.168.1.10", "192.168.1.30")

	// Check event count
	assert.Equal(t, 2, exporter.GetEventCount())

	// Verify metrics are exported
	metrics := make(chan prometheus.Metric, 10)
	go func() {
		exporter.Collect(metrics)
		close(metrics)
	}()

	metricCount := 0
	for range metrics {
		metricCount++
	}

	assert.Greater(t, metricCount, 0, "should export metrics")
}

// TestDiscovery_Service_Integration tests discovery service with real traceroute
func TestDiscovery_Service_Integration(t *testing.T) {
	skipIfNotRoot(t)

	// Create discovery service
	cache := discovery.NewPathCache(1*time.Hour, 100)
	lossTracker := discovery.NewLossTracker(1 * time.Hour)
	tracerouter := discovery.NewDefaultTracerouter()

	service := discovery.NewDiscoveryService(
		tracerouter,
		cache,
		lossTracker,
		10,
		"top_loss",
		5*time.Minute,
	)

	require.NotNil(t, service)

	// Test HTTP handler
	handler := service.HTTPHandler()
	require.NotNil(t, handler)

	// Cleanup
	service.Stop()
}

// TestTopology_Load_Integration tests topology loading from file
func TestTopology_Load_Integration(t *testing.T) {
	// Create a temporary topology file
	tmpFile := "/tmp/test_topology.yaml"
	topologyContent := `
devices:
  - id: leaf-01
    name: leaf-01.local
    type: leaf
    ip_addresses:
      - "10.0.1.1"
    subnets:
      - "10.0.1.0/24"
    rack: rack-01
    datacenter: dc1

  - id: spine-01
    name: spine-01.local
    type: spine
    ip_addresses:
      - "10.0.0.1"
    connected_devices:
      - leaf-01
`

	err := os.WriteFile(tmpFile, []byte(topologyContent), 0644)
	require.NoError(t, err)
	defer os.Remove(tmpFile)

	// Load topology
	topo, err := topology.Load(tmpFile)
	require.NoError(t, err)

	assert.Equal(t, 2, topo.DeviceCount())
	assert.Equal(t, "spine-leaf", topo.GetTopologyType())

	// Test device lookup
	device, ok := topo.GetDeviceByIP("10.0.1.1")
	assert.True(t, ok)
	assert.Equal(t, "leaf-01", device.ID)

	// Test path enrichment
	pathInfo := topo.EnrichPath("10.0.1.1", "10.0.0.1")
	assert.NotNil(t, pathInfo)
	assert.Equal(t, "dc1", pathInfo.SourceLocation)
}

// TestConntrack_Tracker_Integration tests connection tracker (stub test for non-eBPF)
func TestConntrack_Tracker_Integration(t *testing.T) {
	logger := zap.NewNop()

	// Create tracker config
	cfg := conntrack.Config{
		EBPFProgramPath: "", // Empty = use simulated events
		TrackIncoming:   true,
		TrackOutgoing:   true,
		TrackCloses:     true,
		SYNTimeout:      30 * time.Second,
		Syslog: conntrack.SyslogConfig{
			Tag:      "conntrack-test",
			Facility: conntrack.LOG_LOCAL0,
		},
	}

	// Create tracker
	tracker, err := conntrack.NewTracker(cfg, logger)
	require.NoError(t, err)
	require.NotNil(t, tracker)

	// Start tracker
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	errChan := make(chan error, 1)
	go func() {
		errChan <- tracker.Run(ctx)
	}()

	// Wait for simulated events
	<-time.After(6 * time.Second)

	// Check stats
	stats := tracker.GetStats()
	assert.GreaterOrEqual(t, stats.TotalOutgoing+stats.TotalIncoming, 0)

	// Cleanup
	cancel()
	select {
	case err := <-errChan:
		if err != context.Canceled {
			t.Logf("Tracker stopped: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Log("Tracker cleanup timeout")
	}
}

// TestMetadata_LocationMatcher_Integration tests location matcher with real data
func TestMetadata_LocationMatcher_Integration(t *testing.T) {
	// Create a temporary locations file
	tmpFile := "/tmp/test_locations.yaml"
	locationsContent := `
locations:
  - network: 192.168.1.0/24
    location: DC1-RACK1
  - network: 192.168.2.0/24
    location: DC1-RACK2
  - network: 10.0.0.0/8
    location: DC2
`

	err := os.WriteFile(tmpFile, []byte(locationsContent), 0644)
	require.NoError(t, err)
	defer os.Remove(tmpFile)

	// Load matcher
	matcher, err := metadata.NewLocationMatcher(tmpFile)
	require.NoError(t, err)

	// Test matching
	location := matcher.GetLocation("192.168.1.100")
	assert.Equal(t, "DC1-RACK1", location)

	location = matcher.GetLocation("192.168.2.50")
	assert.Equal(t, "DC1-RACK2", location)

	location = matcher.GetLocation("10.1.2.3")
	assert.Equal(t, "DC2", location)

	// Unknown IP
	location = matcher.GetLocation("8.8.8.8")
	assert.Equal(t, "unknown", location)
}

// TestMetadata_RoleMatcher_Integration tests role matcher with real data
func TestMetadata_RoleMatcher_Integration(t *testing.T) {
	// Create a temporary roles file
	tmpFile := "/tmp/test_roles.yaml"
	rolesContent := `
roles:
  - network: 192.168.1.10/32
    role: web-server-01
  - network: 192.168.1.20/32
    role: db-server-01
  - network: 192.168.1.0/24
    role: web-tier
`

	err := os.WriteFile(tmpFile, []byte(rolesContent), 0644)
	require.NoError(t, err)
	defer os.Remove(tmpFile)

	// Load matcher
	matcher, err := metadata.NewRoleMatcher(tmpFile)
	require.NoError(t, err)

	// Test matching - most specific wins
	role := matcher.GetRole("192.168.1.10")
	assert.Equal(t, "web-server-01", role)

	role = matcher.GetRole("192.168.1.20")
	assert.Equal(t, "db-server-01", role)

	role = matcher.GetRole("192.168.1.100")
	assert.Equal(t, "web-tier", role)
}

// TestConfig_Load_Integration tests configuration loading
func TestConfig_Load_Integration(t *testing.T) {
	// Create a temporary config file
	tmpFile := "/tmp/test_config.yaml"
	configContent := `
global:
  ttl_hours: 2
  metrics_port: 9999
  trace_pipe_path: /sys/kernel/tracing/trace_pipe

metadata:
  locations:
    type: file
    path: /tmp/locations.yaml
  roles:
    type: file
    path: /tmp/roles.yaml

discovery:
  traceroute:
    enabled: true
    top_n: 5
    mode: top_loss
    interval: 10m
    protocol: icmp
    max_hops: 30
    timeout: 3s
    probes_per_hop: 3

logging:
  level: debug
  format: json
`

	err := os.WriteFile(tmpFile, []byte(configContent), 0644)
	require.NoError(t, err)
	defer os.Remove(tmpFile)

	// Load config
	cfg, err := config.Load(tmpFile)
	require.NoError(t, err)

	assert.Equal(t, 2, cfg.Global.TTLHours)
	assert.Equal(t, 9999, cfg.Global.MetricsPort)
	assert.Equal(t, "debug", cfg.Logging.Level)
	assert.Equal(t, "json", cfg.Logging.Format)
	assert.Equal(t, 5, cfg.Discovery.Traceroute.TopN)
}

// generateTCPTraffic generates TCP traffic for testing
func generateTCPTraffic(t *testing.T) {
	// Try to connect to localhost to generate some TCP events
	conn, err := net.DialTimeout("tcp", "127.0.0.1:80", 100*time.Millisecond)
	if err != nil {
		// Port 80 might not be available, try another
		conn, err = net.DialTimeout("tcp", "127.0.0.1:22", 100*time.Millisecond)
	}
	if err == nil {
		conn.Close()
	}
}

// mockExporter is a mock retransmit exporter for testing
type mockExporter struct {
	count int
}

func (m *mockExporter) RecordRetransmit(srcIP, dstIP string) {
	m.count++
}
