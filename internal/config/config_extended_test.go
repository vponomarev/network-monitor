package config

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_DefaultConfig(t *testing.T) {
	// Test loading non-existent file returns defaults
	cfg, err := Load("/nonexistent/config.yaml")
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.Equal(t, 3, cfg.Global.TTLHours)
	assert.Equal(t, 9876, cfg.Global.MetricsPort)
	assert.Equal(t, "/sys/kernel/tracing/trace_pipe", cfg.Global.TracePipePath)
}

func TestLoad_FromFile(t *testing.T) {
	// Create temporary config file
	tmpfile, err := os.CreateTemp("", "config_*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpfile.Name())

	yamlContent := `global:
  ttl_hours: 6
  metrics_port: 9090
  trace_pipe_path: /custom/trace_pipe

metadata:
  locations:
    path: /custom/locations.yaml
  roles:
    path: /custom/roles.yaml

discovery:
  traceroute:
    enabled: true
    top_n: 20
    mode: top_loss
    interval: 10m

metrics:
  name: custom_tcp_loss
  default_labels:
    - src_ip
    - dst_ip
  optional_labels:
    - path_id

logging:
  level: debug
  format: console
`
	_, err = tmpfile.WriteString(yamlContent)
	require.NoError(t, err)
	tmpfile.Close()

	cfg, err := Load(tmpfile.Name())
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.Equal(t, 6, cfg.Global.TTLHours)
	assert.Equal(t, 9090, cfg.Global.MetricsPort)
	assert.Equal(t, "/custom/trace_pipe", cfg.Global.TracePipePath)
	assert.Equal(t, "/custom/locations.yaml", cfg.Metadata.Locations.Path)
	assert.Equal(t, "/custom/roles.yaml", cfg.Metadata.Roles.Path)
	assert.Equal(t, 20, cfg.Discovery.Traceroute.TopN)
	assert.Equal(t, "top_loss", cfg.Discovery.Traceroute.Mode)
	assert.Equal(t, "debug", cfg.Logging.Level)
	assert.Equal(t, "console", cfg.Logging.Format)
}

func TestLoad_InvalidYAML(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "config_invalid_*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpfile.Name())

	_, err = tmpfile.WriteString("invalid: yaml: content: [")
	require.NoError(t, err)
	tmpfile.Close()

	_, err = Load(tmpfile.Name())
	assert.Error(t, err)
}

func TestLoad_PartialConfig(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "config_partial_*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpfile.Name())

	// Only override some values
	yamlContent := `global:
  metrics_port: 8080
`
	_, err = tmpfile.WriteString(yamlContent)
	require.NoError(t, err)
	tmpfile.Close()

	cfg, err := Load(tmpfile.Name())
	require.NoError(t, err)

	// Overridden value
	assert.Equal(t, 8080, cfg.Global.MetricsPort)

	// Default values
	assert.Equal(t, 3, cfg.Global.TTLHours)
	assert.Equal(t, "/sys/kernel/tracing/trace_pipe", cfg.Global.TracePipePath)
}

func TestConfig_Validate_InvalidPort(t *testing.T) {
	tests := []struct {
		name string
		port int
	}{
		{"zero", 0},
		{"negative", -1},
		{"too high", 65536},
		{"way too high", 100000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Global.MetricsPort = tt.port
			err := cfg.Validate()
			assert.Error(t, err)
		})
	}
}

func TestConfig_Validate_InvalidTTL(t *testing.T) {
	tests := []struct {
		name string
		ttl  int
	}{
		{"zero", 0},
		{"negative", -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Global.TTLHours = tt.ttl
			err := cfg.Validate()
			assert.Error(t, err)
		})
	}
}

func TestConfig_Validate_InvalidMode(t *testing.T) {
	modes := []string{"invalid", "unknown", "bad_mode", ""}

	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Discovery.Traceroute.Mode = mode
			err := cfg.Validate()
			assert.Error(t, err)
		})
	}
}

func TestConfig_Validate_ValidModes(t *testing.T) {
	modes := []string{"both", "top_loss", "on_demand", "periodic"}

	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Discovery.Traceroute.Mode = mode
			err := cfg.Validate()
			assert.NoError(t, err)
		})
	}
}

func TestConfig_Validate_InvalidInterval(t *testing.T) {
	intervals := []string{"invalid", "100", "abc", ""}

	for _, interval := range intervals {
		t.Run(interval, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Discovery.Traceroute.Interval = interval
			err := cfg.Validate()
			assert.Error(t, err)
		})
	}
}

func TestConfig_Validate_ValidInterval(t *testing.T) {
	intervals := []string{"1s", "5m", "1h", "100ms"}

	for _, interval := range intervals {
		t.Run(interval, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Discovery.Traceroute.Interval = interval
			err := cfg.Validate()
			assert.NoError(t, err)
		})
	}
}

func TestConfig_TTL_Hours(t *testing.T) {
	cfg := DefaultConfig()
	
	assert.Equal(t, 3*time.Hour, cfg.TTL())

	cfg.Global.TTLHours = 1
	assert.Equal(t, 1*time.Hour, cfg.TTL())

	cfg.Global.TTLHours = 24
	assert.Equal(t, 24*time.Hour, cfg.TTL())
}

func TestConfig_EmptyFile(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "config_empty_*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpfile.Name())

	_, err = tmpfile.WriteString("")
	require.NoError(t, err)
	tmpfile.Close()

	cfg, err := Load(tmpfile.Name())
	require.NoError(t, err)

	// Should use defaults
	assert.Equal(t, 3, cfg.Global.TTLHours)
	assert.Equal(t, 9876, cfg.Global.MetricsPort)
}

func TestConfig_OnlyGlobalSection(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "config_global_*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpfile.Name())

	yamlContent := `global:
  ttl_hours: 12
  metrics_port: 8888
`
	_, err = tmpfile.WriteString(yamlContent)
	require.NoError(t, err)
	tmpfile.Close()

	cfg, err := Load(tmpfile.Name())
	require.NoError(t, err)

	assert.Equal(t, 12, cfg.Global.TTLHours)
	assert.Equal(t, 8888, cfg.Global.MetricsPort)
	// Other sections should use defaults
	assert.Equal(t, "info", cfg.Logging.Level)
}

func TestConfig_OnlyMetadataSection(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "config_metadata_*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpfile.Name())

	yamlContent := `metadata:
  locations:
    path: /my/locations.yaml
  roles:
    path: /my/roles.yaml
`
	_, err = tmpfile.WriteString(yamlContent)
	require.NoError(t, err)
	tmpfile.Close()

	cfg, err := Load(tmpfile.Name())
	require.NoError(t, err)

	assert.Equal(t, "/my/locations.yaml", cfg.Metadata.Locations.Path)
	assert.Equal(t, "/my/roles.yaml", cfg.Metadata.Roles.Path)
	// Global should use defaults
	assert.Equal(t, 9876, cfg.Global.MetricsPort)
}

func TestConfig_Validate_ValidConfig(t *testing.T) {
	cfg := DefaultConfig()
	err := cfg.Validate()
	assert.NoError(t, err)
}

func TestConfig_DefaultLabels(t *testing.T) {
	cfg := DefaultConfig()
	
	expectedLabels := []string{
		"src_ip",
		"dst_ip",
		"src_location",
		"dst_location",
		"src_role",
		"dst_role",
	}

	assert.Equal(t, expectedLabels, cfg.Metrics.DefaultLabels)
}

func TestConfig_OptionalLabels(t *testing.T) {
	cfg := DefaultConfig()
	
	expectedLabels := []string{
		"src_network",
		"dst_network",
		"path_id",
	}

	assert.Equal(t, expectedLabels, cfg.Metrics.OptionalLabels)
}

func TestConfig_LoggingDefaults(t *testing.T) {
	cfg := DefaultConfig()
	
	assert.Equal(t, "info", cfg.Logging.Level)
	assert.Equal(t, "json", cfg.Logging.Format)
}

func TestConfig_DiscoveryDefaults(t *testing.T) {
	cfg := DefaultConfig()
	
	assert.True(t, cfg.Discovery.Traceroute.Enabled)
	assert.Equal(t, 10, cfg.Discovery.Traceroute.TopN)
	assert.Equal(t, "both", cfg.Discovery.Traceroute.Mode)
	assert.Equal(t, "5m", cfg.Discovery.Traceroute.Interval)
}
