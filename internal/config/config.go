package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
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
	MetricsAddr   string `yaml:"metrics_addr"` // Bind address (default: "0.0.0.0")
	AuthToken     string `yaml:"auth_token"`   // Optional auth token for /metrics and /api/*
	TracePipePath string `yaml:"trace_pipe_path"`
	// LossSource selects the TCP-loss data source:
	//   "ebpf"      — eBPF tracepoint tcp_retransmit_skb via ring buffer (default, production)
	//   "tracepipe" — legacy text scrape of trace_pipe (fallback/debug)
	LossSource string `yaml:"loss_source"`
}

// MetadataSourceConfig описывает HTTP источник для обновления metadata
type MetadataSourceConfig struct {
	URL          string `yaml:"url"`
	PollInterval string `yaml:"poll_interval"` // "20m", "1h"
	Timeout      string `yaml:"timeout"`       // "10s"
}

// PollIntervalDuration возвращает интервал как time.Duration
func (m *MetadataSourceConfig) PollIntervalDuration() time.Duration {
	if m.PollInterval == "" {
		return 20 * time.Minute // дефолт
	}
	d, err := time.ParseDuration(m.PollInterval)
	if err != nil {
		return 20 * time.Minute
	}
	return d
}

// TimeoutDuration возвращает timeout как time.Duration
func (m *MetadataSourceConfig) TimeoutDuration() time.Duration {
	if m.Timeout == "" {
		return 10 * time.Second
	}
	d, err := time.ParseDuration(m.Timeout)
	if err != nil {
		return 10 * time.Second
	}
	return d
}

// FileMetadataConfig описывает файл + опциональный update source
type FileMetadataConfig struct {
	Path         string                `yaml:"path"`          // обязательный
	UpdateSource *MetadataSourceConfig `yaml:"update_source"` // опционально
}

// MetadataConfig holds metadata source configuration
type MetadataConfig struct {
	Locations FileMetadataConfig `yaml:"locations"`
	Roles     FileMetadataConfig `yaml:"roles"`
	Topology  FileMetadataConfig `yaml:"topology"`
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
	Name           string            `yaml:"name"`
	DefaultLabels  []string          `yaml:"default_labels"`
	OptionalLabels []string          `yaml:"optional_labels"`
	Cardinality    CardinalityConfig `yaml:"cardinality"`
}

// CardinalityConfig controls the label granularity and the hard cap on the
// number of active loss series exported to Prometheus.
type CardinalityConfig struct {
	// Level: "ip" | "role" | "network".
	//   ip      — label every series with full src_ip/dst_ip (unbounded cardinality)
	//   network — aggregate to /24 networks (no per-IP labels)
	//   role    — aggregate to location/role/vrf (no IP, no network) [default]
	Level string `yaml:"level"`
	// MaxSeries caps the number of distinct active series. 0 = unlimited.
	MaxSeries int `yaml:"max_series"`
}

// LoggingConfig holds logging settings
type LoggingConfig struct {
	Level      string `yaml:"level"`
	Format     string `yaml:"format"`
	OutputPath string `yaml:"output_path"` // Empty = stdout/stderr
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
	Enabled         bool  `yaml:"enabled"`
	TrackIncoming   bool  `yaml:"track_incoming"`
	TrackOutgoing   bool  `yaml:"track_outgoing"`
	FilterPorts     []int `yaml:"filter_ports"`
	EventBufferSize int   `yaml:"event_buffer_size"` // Default: 10000
}

// DefaultConfig returns a configuration with default values
func DefaultConfig() *Config {
	return &Config{
		Global: GlobalConfig{
			TTLHours:      3,
			MetricsPort:   9876,
			MetricsAddr:   "0.0.0.0",
			TracePipePath: "/sys/kernel/tracing/trace_pipe",
			LossSource:    "ebpf",
		},
		Metadata: MetadataConfig{
			Locations: FileMetadataConfig{
				Path: "locations.yaml",
			},
			Roles: FileMetadataConfig{
				Path: "roles.yaml",
			},
			Topology: FileMetadataConfig{
				Path: "topology.yaml",
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
			Cardinality: CardinalityConfig{
				Level:     "role",
				MaxSeries: 10000,
			},
		},
		Logging: LoggingConfig{
			Level:      "info",
			Format:     "json",
			OutputPath: "", // Default to stdout/stderr
		},
		Connections: ConnectionsConfig{
			Enabled:       true,
			TrackIncoming: true,
			TrackOutgoing: true,
		},
	}
}

// Load loads configuration from YAML file
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	// Resolve relative paths relative to config file directory
	configDir := filepath.Dir(path)
	cfg.resolveRelativePaths(configDir)

	// Override auth token from environment variable if not set in config
	if cfg.Global.AuthToken == "" {
		cfg.Global.AuthToken = os.Getenv("NETMON_AUTH_TOKEN")
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}

	return cfg, nil
}

// resolveRelativePaths converts relative paths to absolute paths relative to config file directory
func (c *Config) resolveRelativePaths(configDir string) {
	// Resolve metadata paths
	if c.Metadata.Locations.Path != "" && !filepath.IsAbs(c.Metadata.Locations.Path) {
		c.Metadata.Locations.Path = filepath.Join(configDir, c.Metadata.Locations.Path)
	}
	if c.Metadata.Roles.Path != "" && !filepath.IsAbs(c.Metadata.Roles.Path) {
		c.Metadata.Roles.Path = filepath.Join(configDir, c.Metadata.Roles.Path)
	}
	if c.Metadata.Topology.Path != "" && !filepath.IsAbs(c.Metadata.Topology.Path) {
		c.Metadata.Topology.Path = filepath.Join(configDir, c.Metadata.Topology.Path)
	}

	// Resolve topology path (legacy TopologyConfig)
	if c.Topology.Path != "" && !filepath.IsAbs(c.Topology.Path) {
		c.Topology.Path = filepath.Join(configDir, c.Topology.Path)
	}

	// Resolve log output path
	if c.Logging.OutputPath != "" && !filepath.IsAbs(c.Logging.OutputPath) {
		c.Logging.OutputPath = filepath.Join(configDir, c.Logging.OutputPath)
	}
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

	// Validate metrics bind address
	if c.Global.MetricsAddr != "" {
		if ip := net.ParseIP(c.Global.MetricsAddr); ip == nil {
			return fmt.Errorf("invalid metrics_addr: %q is not a valid IP address", c.Global.MetricsAddr)
		}
	}

	// Validate metrics cardinality settings
	if c.Metrics.Cardinality.Level == "" {
		c.Metrics.Cardinality.Level = "role" // default; keeps old configs working
	}
	validCardinalityLevels := map[string]bool{"ip": true, "role": true, "network": true}
	if !validCardinalityLevels[c.Metrics.Cardinality.Level] {
		return fmt.Errorf("invalid metrics.cardinality.level: %s (valid: ip, role, network)", c.Metrics.Cardinality.Level)
	}
	if c.Metrics.Cardinality.MaxSeries < 0 {
		return fmt.Errorf("invalid metrics.cardinality.max_series: must be >= 0 (0 = unlimited)")
	}

	if c.Global.TracePipePath == "" {
		return fmt.Errorf("trace_pipe_path is required")
	}

	// Validate loss source (empty defaults to ebpf for backward compatibility)
	if c.Global.LossSource == "" {
		c.Global.LossSource = "ebpf"
	}
	validLossSources := map[string]bool{"ebpf": true, "tracepipe": true}
	if !validLossSources[c.Global.LossSource] {
		return fmt.Errorf("invalid loss_source: %s (valid: ebpf, tracepipe)", c.Global.LossSource)
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
	if c.Metadata.Topology.Path == "" {
		return fmt.Errorf("metadata.topology.path is required")
	}

	// Validate topology path if enabled (legacy TopologyConfig)
	if c.Topology.Enabled && c.Topology.Path == "" {
		return fmt.Errorf("topology.path is required when topology is enabled")
	}

	// Validate update sources if specified
	if c.Metadata.Locations.UpdateSource != nil {
		if c.Metadata.Locations.UpdateSource.URL == "" {
			return fmt.Errorf("metadata.locations.update_source.url is required")
		}
	}
	if c.Metadata.Roles.UpdateSource != nil {
		if c.Metadata.Roles.UpdateSource.URL == "" {
			return fmt.Errorf("metadata.roles.update_source.url is required")
		}
	}
	if c.Metadata.Topology.UpdateSource != nil {
		if c.Metadata.Topology.UpdateSource.URL == "" {
			return fmt.Errorf("metadata.topology.update_source.url is required")
		}
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

	// Validate log output path if specified
	if c.Logging.OutputPath != "" {
		// Check if directory exists and is writable
		dir := filepath.Dir(c.Logging.OutputPath)
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			return fmt.Errorf("log output directory does not exist: %s", dir)
		}
		// Try to create/truncate the file to check write permissions
		f, err := os.OpenFile(c.Logging.OutputPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("cannot write to log file %s: %w", c.Logging.OutputPath, err)
		}
		f.Close()
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
