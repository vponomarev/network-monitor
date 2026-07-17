package config

import (
	"testing"
	"time"
)

func TestDefaultConfig_ConntrackRetention(t *testing.T) {
	cfg := DefaultConfig()
	if got := cfg.Connections.StateTTLDuration(); got != 24*time.Hour {
		t.Fatalf("StateTTLDuration() = %v, want 24h", got)
	}
	if got := cfg.Connections.CleanupIntervalDuration(); got != time.Minute {
		t.Fatalf("CleanupIntervalDuration() = %v, want 1m", got)
	}
	if cfg.Connections.MaxTrackedConnections != 10240 {
		t.Fatalf("MaxTrackedConnections = %d, want 10240", cfg.Connections.MaxTrackedConnections)
	}
	if cfg.Connections.MaxPendingConnections != 16384 {
		t.Fatalf("MaxPendingConnections = %d, want 16384", cfg.Connections.MaxPendingConnections)
	}
}

func TestConfigValidate_ConntrackRetention(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{"zero ttl", func(c *Config) { c.Connections.StateTTL = "0s" }},
		{"invalid interval", func(c *Config) { c.Connections.CleanupInterval = "often" }},
		{"zero event buffer", func(c *Config) { c.Connections.EventBufferSize = 0 }},
		{"zero tracked limit", func(c *Config) { c.Connections.MaxTrackedConnections = 0 }},
		{"zero pending limit", func(c *Config) { c.Connections.MaxPendingConnections = 0 }},
		{"excessive tracked limit", func(c *Config) { c.Connections.MaxTrackedConnections = 1_000_001 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tt.mutate(cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("Validate() succeeded, want error")
			}
		})
	}
}
