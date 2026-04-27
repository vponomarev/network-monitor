package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds the application configuration
type Config struct {
	Global      GlobalConfig      `yaml:"global"`
	Metadata    MetadataConfig    `yaml:"metadata"`
	Discovery   DiscoveryConfig   `yaml:"discovery"`
	Topology    TopologyConfig    `yaml:"topology"`
	Metrics     MetricsConfig     `yaml:"metrics"`
	Logging     LoggingConfig     `yaml:"logging"`
	Connections ConnectionsConfig `yaml:"connections"`
	PacketLoss  PacketLossConfig  `yaml:"packet_loss"`
	Latency     LatencyConfig     `yaml:"latency"`
	Bandwidth   BandwidthConfig   `yaml:"bandwidth"`
	DNS         DNSConfig         `yaml:"dns"`
}

// GlobalConfig holds global settings
type GlobalConfig struct {
	TTLHours      int    `yaml:"ttl_hours"`
	MetricsPort   int    `yaml:"metrics_port"`
	TracePipePath string `yaml:"trace_pipe_path"`
}

// MetadataConfig holds metadata source configuration
type MetadataConfig struct {
	Locations FileSourceConfig `yaml:"locations"`
	Roles     FileSourceConfig `yaml:"roles"`
}

// FileSourceConfig holds file-based source configuration
type FileSourceConfig struct {
	Type string `yaml:"type"`
	Path string `yaml:"path"`
}

// DiscoveryConfig holds discovery settings
type DiscoveryConfig struct {
	Traceroute TracerouteConfig `yaml:"traceroute"`
}

// TopologyConfig holds topology settings
type TopologyConfig struct {
	Enabled bool   `yaml:"enabled"`
	Path    string `yaml:"path"`
}

// TracerouteConfig holds traceroute settings
type TracerouteConfig struct {
	Enabled      bool   `yaml:"enabled"`
	TopN         int    `yaml:"top_n"`
	Mode         string `yaml:"mode"`
	Interval     string `yaml:"interval"`
	Protocol     string `yaml:"protocol"`
	DstPort      int    `yaml:"dst_port"`
	SrcPort      int    `yaml:"src_port"`
	TCPFlags     string `yaml:"tcp_flags"`
	MaxHops      int    `yaml:"max_hops"`
	Timeout      string `yaml:"timeout"`
	ProbesPerHop int    `yaml:"probes_per_hop"`
}

// MetricsConfig holds metrics settings
type MetricsConfig struct {
	Name           string   `yaml:"name"`
	DefaultLabels  []string `yaml:"default_labels"`
	OptionalLabels []string `yaml:"optional_labels"`
}

// LoggingConfig holds logging settings
type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

// PacketLossConfig holds packet loss monitoring configuration (for other modules)
type PacketLossConfig struct {
	Enabled          bool     `yaml:"enabled"`
	Interfaces       []string `yaml:"interfaces"`
	ThresholdPercent float64  `yaml:"threshold_percent"`
	WindowSize       int      `yaml:"window_size"`
	AlertInterval    string   `yaml:"alert_interval"`
}

// LatencyConfig holds latency monitoring configuration (for other modules)
type LatencyConfig struct {
	Enabled  bool     `yaml:"enabled"`
	Targets  []string `yaml:"targets"`
	Interval string   `yaml:"interval"`
	Timeout  string   `yaml:"timeout"`
}

// BandwidthConfig holds bandwidth monitoring configuration (for other modules)
type BandwidthConfig struct {
	Enabled    bool     `yaml:"enabled"`
	Interfaces []string `yaml:"interfaces"`
	Interval   string   `yaml:"interval"`
}

// DNSConfig holds DNS monitoring configuration (for other modules)
type DNSConfig struct {
	Enabled    bool     `yaml:"enabled"`
	Interfaces []string `yaml:"interfaces"`
	Port       int      `yaml:"port"`
	Interval   string   `yaml:"interval"`
}

// ConnectionsConfig holds connection tracking configuration (for other modules)
type ConnectionsConfig struct {
	Enabled       bool  `yaml:"enabled"`
	TrackIncoming bool  `yaml:"track_incoming"`
	TrackOutgoing bool  `yaml:"track_outgoing"`
	FilterPorts   []int `yaml:"filter_ports"`
}

// DefaultConfig returns a configuration with default values
func DefaultConfig() *Config {
	return &Config{
		Global: GlobalConfig{
			TTLHours:      3,
			MetricsPort:   9876,
			TracePipePath: "/sys/kernel/tracing/trace_pipe",
		},
		Metadata: MetadataConfig{
			Locations: FileSourceConfig{
				Type: "file",
				Path: "locations.yaml",
			},
			Roles: FileSourceConfig{
				Type: "file",
				Path: "roles.yaml",
			},
		},
		Discovery: DiscoveryConfig{
			Traceroute: TracerouteConfig{
				Enabled:      true,
				TopN:         10,
				Mode:         "both",
				Interval:     "5m",
				Protocol:     "icmp",
				MaxHops:      30,
				Timeout:      "3s",
				ProbesPerHop: 3,
			},
		},
		Topology: TopologyConfig{
			Enabled: false,
			Path:    "topology.yaml",
		},
		Metrics: MetricsConfig{
			Name: "netmon_tcp_loss_total",
			DefaultLabels: []string{
				"src_ip",
				"dst_ip",
				"src_location",
				"dst_location",
				"src_role",
				"dst_role",
			},
			OptionalLabels: []string{
				"src_network",
				"dst_network",
				"path_id",
			},
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "json",
		},
	}
}

// Load loads configuration from YAML file
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Return default config if file doesn't exist
			return cfg, nil
		}
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}

	return cfg, nil
}

// Validate validates the configuration
func (c *Config) Validate() error {
	// Validate global settings
	if c.Global.MetricsPort < 1 || c.Global.MetricsPort > 65535 {
		return fmt.Errorf("invalid metrics_port: must be between 1 and 65535")
	}

	if c.Global.TTLHours < 1 {
		return fmt.Errorf("invalid ttl_hours: must be at least 1")
	}

	if c.Global.TracePipePath == "" {
		return fmt.Errorf("trace_pipe_path is required")
	}

	// Validate discovery settings
	validModes := map[string]bool{"both": true, "top_loss": true, "on_demand": true, "periodic": true}
	if !validModes[c.Discovery.Traceroute.Mode] {
		return fmt.Errorf("invalid discovery mode: %s (valid: both, top_loss, on_demand, periodic)", c.Discovery.Traceroute.Mode)
	}

	if _, err := time.ParseDuration(c.Discovery.Traceroute.Interval); err != nil {
		return fmt.Errorf("invalid discovery interval: %w", err)
	}

	if c.Discovery.Traceroute.TopN < 1 || c.Discovery.Traceroute.TopN > 100 {
		return fmt.Errorf("invalid traceroute top_n: must be between 1 and 100")
	}

	// Validate traceroute protocol
	validProtocols := map[string]bool{"icmp": true, "udp": true, "tcp": true}
	if !validProtocols[c.Discovery.Traceroute.Protocol] {
		return fmt.Errorf("invalid traceroute protocol: %s (valid: icmp, udp, tcp)", c.Discovery.Traceroute.Protocol)
	}

	if c.Discovery.Traceroute.MaxHops < 1 || c.Discovery.Traceroute.MaxHops > 64 {
		return fmt.Errorf("invalid traceroute max_hops: must be between 1 and 64")
	}

	if _, err := time.ParseDuration(c.Discovery.Traceroute.Timeout); err != nil {
		return fmt.Errorf("invalid traceroute timeout: %w", err)
	}

	if c.Discovery.Traceroute.ProbesPerHop < 1 || c.Discovery.Traceroute.ProbesPerHop > 10 {
		return fmt.Errorf("invalid traceroute probes_per_hop: must be between 1 and 10")
	}

	// Validate metadata paths
	if c.Metadata.Locations.Path == "" {
		return fmt.Errorf("metadata.locations.path is required")
	}

	if c.Metadata.Roles.Path == "" {
		return fmt.Errorf("metadata.roles.path is required")
	}

	// Validate topology path if enabled
	if c.Topology.Enabled && c.Topology.Path == "" {
		return fmt.Errorf("topology.path is required when topology is enabled")
	}

	// Validate logging settings
	validLogLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	if !validLogLevels[c.Logging.Level] {
		return fmt.Errorf("invalid logging level: %s (valid: debug, info, warn, error)", c.Logging.Level)
	}

	validLogFormats := map[string]bool{"json": true, "console": true}
	if !validLogFormats[c.Logging.Format] {
		return fmt.Errorf("invalid logging format: %s (valid: json, console)", c.Logging.Format)
	}

	// Validate packet loss config
	if c.PacketLoss.Enabled {
		if c.PacketLoss.ThresholdPercent < 0 || c.PacketLoss.ThresholdPercent > 100 {
			return fmt.Errorf("invalid packet_loss threshold_percent: must be between 0 and 100")
		}
		if c.PacketLoss.WindowSize < 10 || c.PacketLoss.WindowSize > 1000 {
			return fmt.Errorf("invalid packet_loss window_size: must be between 10 and 1000")
		}
	}

	// Validate latency config
	if c.Latency.Enabled {
		if len(c.Latency.Targets) == 0 {
			return fmt.Errorf("latency.targets is required when latency is enabled")
		}
		if _, err := time.ParseDuration(c.Latency.Interval); err != nil {
			return fmt.Errorf("invalid latency interval: %w", err)
		}
	}

	// Validate bandwidth config
	if c.Bandwidth.Enabled {
		if len(c.Bandwidth.Interfaces) == 0 {
			return fmt.Errorf("bandwidth.interfaces is required when bandwidth is enabled")
		}
		if _, err := time.ParseDuration(c.Bandwidth.Interval); err != nil {
			return fmt.Errorf("invalid bandwidth interval: %w", err)
		}
	}

	return nil
}

// TTL returns the TTL duration
func (c *Config) TTL() time.Duration {
	return time.Duration(c.Global.TTLHours) * time.Hour
}

// PacketLossInterval returns the alert interval as time.Duration
func (c *PacketLossConfig) AlertIntervalDuration() time.Duration {
	if c.AlertInterval == "" {
		return time.Minute
	}
	d, err := time.ParseDuration(c.AlertInterval)
	if err != nil {
		return time.Minute
	}
	return d
}

// LatencyInterval returns the interval as time.Duration
func (c *LatencyConfig) IntervalDuration() time.Duration {
	if c.Interval == "" {
		return 5 * time.Minute
	}
	d, err := time.ParseDuration(c.Interval)
	if err != nil {
		return 5 * time.Minute
	}
	return d
}

// LatencyTimeout returns the timeout as time.Duration
func (c *LatencyConfig) TimeoutDuration() time.Duration {
	if c.Timeout == "" {
		return 5 * time.Second
	}
	d, err := time.ParseDuration(c.Timeout)
	if err != nil {
		return 5 * time.Second
	}
	return d
}

// BandwidthInterval returns the interval as time.Duration
func (c *BandwidthConfig) IntervalDuration() time.Duration {
	if c.Interval == "" {
		return time.Minute
	}
	d, err := time.ParseDuration(c.Interval)
	if err != nil {
		return time.Minute
	}
	return d
}

// DNSInterval returns the interval as time.Duration
func (c *DNSConfig) IntervalDuration() time.Duration {
	if c.Interval == "" {
		return time.Minute
	}
	d, err := time.ParseDuration(c.Interval)
	if err != nil {
		return time.Minute
	}
	return d
}
