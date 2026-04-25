package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	require.NotNil(t, cfg)
	assert.Equal(t, 3, cfg.Global.TTLHours)
	assert.Equal(t, 9876, cfg.Global.MetricsPort)
	assert.Equal(t, "/sys/kernel/tracing/trace_pipe", cfg.Global.TracePipePath)
	assert.Equal(t, "locations.yaml", cfg.Metadata.Locations.Path)
	assert.Equal(t, "roles.yaml", cfg.Metadata.Roles.Path)
	assert.Equal(t, "netmon_tcp_loss_total", cfg.Metrics.Name)
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name      string
		modifyCfg func(*Config)
		wantErr   bool
	}{
		{
			name:      "valid config",
			modifyCfg: func(c *Config) {},
			wantErr:   false,
		},
		{
			name: "invalid port",
			modifyCfg: func(c *Config) {
				c.Global.MetricsPort = 0
			},
			wantErr: true,
		},
		{
			name: "invalid TTL",
			modifyCfg: func(c *Config) {
				c.Global.TTLHours = 0
			},
			wantErr: true,
		},
		{
			name: "invalid mode",
			modifyCfg: func(c *Config) {
				c.Discovery.Traceroute.Mode = "invalid"
			},
			wantErr: true,
		},
		{
			name: "invalid interval",
			modifyCfg: func(c *Config) {
				c.Discovery.Traceroute.Interval = "invalid"
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tt.modifyCfg(cfg)
			err := cfg.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestConfig_TTL(t *testing.T) {
	cfg := DefaultConfig()
	assert.Equal(t, 3*time.Hour, cfg.TTL())

	cfg.Global.TTLHours = 6
	assert.Equal(t, 6*time.Hour, cfg.TTL())
}
