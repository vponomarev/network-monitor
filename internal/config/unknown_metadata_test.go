package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnknownMetadataDefaults(t *testing.T) {
	cfg := DefaultConfig()
	assert.True(t, cfg.Metadata.Unknown.Enabled)
	assert.Equal(t, "3h", cfg.Metadata.Unknown.TTL)
	assert.Equal(t, 10000, cfg.Metadata.Unknown.MaxIPs)
	assert.Equal(t, 3*time.Hour, cfg.Metadata.Unknown.TTLDuration())
}

func TestUnknownMetadataDefaultsSurviveLegacyYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`metadata:
  locations: {path: locations.yaml}
  roles: {path: roles.yaml}
  topology: {path: topology.yaml}
`), 0o600))
	cfg, err := Load(path)
	require.NoError(t, err)
	assert.True(t, cfg.Metadata.Unknown.Enabled)
	assert.Equal(t, "3h", cfg.Metadata.Unknown.TTL)
	assert.Equal(t, 10000, cfg.Metadata.Unknown.MaxIPs)
}

func TestUnknownMetadataValidation(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Metadata.Unknown.TTL = "45m"
		cfg.Metadata.Unknown.MaxIPs = 500
		require.NoError(t, cfg.Validate())
		assert.Equal(t, 45*time.Minute, cfg.Metadata.Unknown.TTLDuration())
	})

	t.Run("invalid ttl", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Metadata.Unknown.TTL = "forever"
		assert.ErrorContains(t, cfg.Validate(), "metadata.unknown.ttl")
	})

	t.Run("invalid max ips", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Metadata.Unknown.MaxIPs = 0
		assert.ErrorContains(t, cfg.Validate(), "metadata.unknown.max_ips")
	})

	t.Run("disabled ignores limits", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Metadata.Unknown.Enabled = false
		cfg.Metadata.Unknown.TTL = ""
		cfg.Metadata.Unknown.MaxIPs = 0
		require.NoError(t, cfg.Validate())
	})
}
