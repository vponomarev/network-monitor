// Package e2e contains end-to-end tests for Network Monitor
// These tests require a full system setup with root privileges
// Run with: sudo go test -v ./tests/e2e/... -tags=e2e
//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	binaryPath   = "../../bin/netmon"
	configPath   = "../../configs/netmon.yaml.example"
	metricsPort  = 19090
	startupDelay = 2 * time.Second
)

// TestE2E_FullSystem tests the complete system
func TestE2E_FullSystem(t *testing.T) {
	// Skip if binary doesn't exist
	if _, err := exec.LookPath(binaryPath); err != nil {
		t.Skipf("Binary not found: %s", binaryPath)
	}

	// Start netmon
	cmd := exec.Command(binaryPath, "--config", configPath)
	cmd.Env = append(cmd.Environ(),
		fmt.Sprintf("NETMON_METRICS_PROMETHEUS_PORT=%d", metricsPort),
	)

	err := cmd.Start()
	require.NoError(t, err)

	// Ensure cleanup
	defer func() {
		cmd.Process.Kill()
		cmd.Wait()
	}()

	// Wait for startup
	time.Sleep(startupDelay)

	// Test metrics endpoint
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/metrics", metricsPort))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	// Check for expected metrics
	bodyStr := string(body)
	assert.Contains(t, bodyStr, "netmon_")
}

// TestE2E_PacketLoss tests packet loss monitoring
func TestE2E_PacketLoss(t *testing.T) {
	if _, err := exec.LookPath("../../bin/pktloss"); err != nil {
		t.Skip("pktloss binary not found")
	}

	// Run pktloss briefly
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "../../bin/pktloss", "-i", "lo")
	output, err := cmd.CombinedOutput()

	// Expected to fail without root, but should start
	if err != nil {
		assert.Contains(t, string(output), "requires root")
	}
}

// TestE2E_CliHelp tests CLI help output
func TestE2E_CliHelp(t *testing.T) {
	tests := []struct {
		binary string
		flag   string
	}{
		{"../../bin/netmon", "--help"},
		{"../../bin/pktloss", "--help"},
		{"../../bin/conntrack", "--help"},
	}

	for _, tt := range tests {
		t.Run(tt.binary, func(t *testing.T) {
			cmd := exec.Command(tt.binary, tt.flag)
			output, err := cmd.CombinedOutput()
			require.NoError(t, err)

			outputStr := string(output)
			assert.NotEmpty(t, outputStr)
			assert.Contains(t, outputStr, "Usage:")
		})
	}
}

// TestE2E_Version tests version output
func TestE2E_Version(t *testing.T) {
	binaries := []string{
		"../../bin/netmon",
		"../../bin/pktloss",
		"../../bin/conntrack",
	}

	for _, binary := range binaries {
		t.Run(binary, func(t *testing.T) {
			cmd := exec.Command(binary, "--version")
			output, err := cmd.CombinedOutput()
			require.NoError(t, err)

			outputStr := strings.TrimSpace(string(output))
			assert.NotEmpty(t, outputStr)
		})
	}
}

// TestE2E_InvalidConfig tests handling of invalid config
func TestE2E_InvalidConfig(t *testing.T) {
	cmd := exec.Command(binaryPath, "--config", "/nonexistent/config.yaml")
	output, err := cmd.CombinedOutput()

	assert.Error(t, err)
	assert.Contains(t, string(output), "failed to load config")
}
