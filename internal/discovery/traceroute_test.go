package discovery

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestDefaultTracerouteConfig(t *testing.T) {
	config := DefaultTracerouteConfig()

	require.NotNil(t, config)
	assert.Equal(t, 30, config.MaxHops)
	assert.Equal(t, 3*time.Second, config.Timeout)
	assert.Equal(t, 3, config.ProbesPerHop)
	assert.Equal(t, 1, config.StartTTL)
	assert.Equal(t, "icmp", config.Protocol)
	assert.Equal(t, 33434, config.DstPort)
}

func TestTracerouteFactory_Create(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultTracerouteConfig()
	factory := NewTracerouteFactory(config, logger)

	require.NotNil(t, factory)

	// On macOS, this should return an error
	_, err := factory.Create("icmp")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestTraceroutePool_NonLinux(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultTracerouteConfig()
	factory := NewTracerouteFactory(config, logger)
	pool := NewTraceroutePool(factory, 5)

	require.NotNil(t, pool)

	ctx := context.Background()
	_, err := pool.Trace(ctx, "8.8.8.8")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")

	_, err = pool.TraceBatch(ctx, []string{"8.8.8.8", "1.1.1.1"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestTracerouteResult_JSON(t *testing.T) {
	result := &TracerouteResult{
		Destination: "8.8.8.8",
		Hops: []HopResult{
			{
				TTL:      1,
				IP:       "192.168.1.1",
				Hostname: "gateway.local",
				RTT:      1 * time.Millisecond,
				Lost:     false,
				Timeout:  false,
			},
			{
				TTL:      2,
				IP:       "10.0.0.1",
				Hostname: "",
				RTT:      5 * time.Millisecond,
				Lost:     false,
				Timeout:  false,
			},
			{
				TTL:      3,
				IP:       "8.8.8.8",
				Hostname: "dns.google",
				RTT:      10 * time.Millisecond,
				Lost:     false,
				Timeout:  false,
			},
		},
		Completed: true,
		Duration:  50 * time.Millisecond,
	}

	require.NotNil(t, result)
	assert.Equal(t, "8.8.8.8", result.Destination)
	assert.Equal(t, 3, len(result.Hops))
	assert.True(t, result.Completed)
	assert.Greater(t, result.Duration, time.Duration(0))

	// Verify hop data
	assert.Equal(t, "192.168.1.1", result.Hops[0].IP)
	assert.Equal(t, "gateway.local", result.Hops[0].Hostname)
	assert.False(t, result.Hops[0].Lost)
	assert.False(t, result.Hops[0].Timeout)

	assert.Equal(t, "8.8.8.8", result.Hops[2].IP)
	assert.Equal(t, "dns.google", result.Hops[2].Hostname)
}

func TestTracerouteResult_Incomplete(t *testing.T) {
	result := &TracerouteResult{
		Destination: "192.0.2.1",
		Hops: []HopResult{
			{
				TTL:     1,
				IP:      "192.168.1.1",
				Lost:    false,
				Timeout: false,
			},
			{
				TTL:     2,
				Lost:    true,
				Timeout: true,
			},
			{
				TTL:     3,
				Lost:    true,
				Timeout: true,
			},
		},
		Completed: false,
		Duration:  10 * time.Second,
	}

	require.NotNil(t, result)
	assert.False(t, result.Completed)
	assert.Equal(t, 3, len(result.Hops))

	// Verify timeout hops
	assert.True(t, result.Hops[1].Lost)
	assert.True(t, result.Hops[1].Timeout)
	assert.Empty(t, result.Hops[1].IP)
}

func TestTracerouteConfig_Custom(t *testing.T) {
	config := &TracerouteConfig{
		MaxHops:      15,
		Timeout:      5 * time.Second,
		ProbesPerHop: 5,
		StartTTL:     1,
		Protocol:     "udp",
		DstPort:      8080,
	}

	assert.Equal(t, 15, config.MaxHops)
	assert.Equal(t, 5*time.Second, config.Timeout)
	assert.Equal(t, 5, config.ProbesPerHop)
	assert.Equal(t, "udp", config.Protocol)
	assert.Equal(t, 8080, config.DstPort)
}

func TestTracerouteFactory_Protocols(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultTracerouteConfig()
	factory := NewTracerouteFactory(config, logger)

	// Test different protocols (all should fail on macOS)
	protocols := []string{"icmp", "udp", ""}

	for _, proto := range protocols {
		_, err := factory.Create(proto)
		assert.Error(t, err, "Protocol: %s", proto)
	}
}

func TestTraceroutePool_Concurrency(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultTracerouteConfig()
	factory := NewTracerouteFactory(config, logger)

	// Test different concurrency levels
	concurrencyLevels := []int{1, 5, 10, 20}

	for _, level := range concurrencyLevels {
		pool := NewTraceroutePool(factory, level)
		require.NotNil(t, pool)
		assert.Equal(t, level, pool.maxConcurrent)
	}
}

func TestHopResult_Fields(t *testing.T) {
	hop := HopResult{
		TTL:       5,
		IP:        "10.0.0.1",
		Hostname:  "router.example.com",
		RTT:       15 * time.Millisecond,
		Lost:      false,
		Timeout:   false,
		ProbeSent: time.Now(),
	}

	assert.Equal(t, 5, hop.TTL)
	assert.Equal(t, "10.0.0.1", hop.IP)
	assert.Equal(t, "router.example.com", hop.Hostname)
	assert.Equal(t, 15*time.Millisecond, hop.RTT)
	assert.False(t, hop.Lost)
	assert.False(t, hop.Timeout)
	assert.WithinDuration(t, time.Now(), hop.ProbeSent, 1*time.Second)
}

func TestTracerouteResult_Empty(t *testing.T) {
	result := &TracerouteResult{
		Destination: "8.8.8.8",
		Hops:        []HopResult{},
		Completed:   false,
		Duration:    0,
	}

	assert.Empty(t, result.Hops)
	assert.False(t, result.Completed)
	assert.Equal(t, time.Duration(0), result.Duration)
}

func TestTracerouteConfig_EdgeCases(t *testing.T) {
	// Zero values
	config := &TracerouteConfig{
		MaxHops:      0,
		Timeout:      0,
		ProbesPerHop: 0,
		StartTTL:     0,
	}

	assert.Equal(t, 0, config.MaxHops)
	assert.Equal(t, time.Duration(0), config.Timeout)
	assert.Equal(t, 0, config.ProbesPerHop)
	assert.Equal(t, 0, config.StartTTL)

	// Large values
	config2 := &TracerouteConfig{
		MaxHops:      255,
		Timeout:      60 * time.Second,
		ProbesPerHop: 100,
	}

	assert.Equal(t, 255, config2.MaxHops)
	assert.Equal(t, 60*time.Second, config2.Timeout)
	assert.Equal(t, 100, config2.ProbesPerHop)
}

func TestTracerouteFactory_NilConfig(t *testing.T) {
	logger := zap.NewNop()

	// Should use defaults when config is nil
	factory := NewTracerouteFactory(nil, logger)
	require.NotNil(t, factory)
	assert.NotNil(t, factory.config)
	assert.Equal(t, 30, factory.config.MaxHops)
}

func TestTraceroutePool_NilFactory(t *testing.T) {
	// Should handle nil factory gracefully
	pool := NewTraceroutePool(nil, 5)
	require.NotNil(t, pool)
	assert.Nil(t, pool.factory)
	assert.Equal(t, 5, pool.maxConcurrent)
}

func TestHopResult_LostTimeout(t *testing.T) {
	// Test different combinations of lost/timeout
	hops := []HopResult{
		{Lost: true, Timeout: true},   // No response
		{Lost: true, Timeout: false},  // Error but not timeout
		{Lost: false, Timeout: true},  // Should not happen normally
		{Lost: false, Timeout: false}, // Success
	}

	assert.True(t, hops[0].Lost)
	assert.True(t, hops[0].Timeout)

	assert.True(t, hops[1].Lost)
	assert.False(t, hops[1].Timeout)

	assert.False(t, hops[2].Lost)
	assert.True(t, hops[2].Timeout)

	assert.False(t, hops[3].Lost)
	assert.False(t, hops[3].Timeout)
}

func TestTracerouteResult_HopCount(t *testing.T) {
	// Test various hop counts
	testCases := []int{0, 1, 5, 15, 30, 50}

	for _, count := range testCases {
		hops := make([]HopResult, count)
		for i := range hops {
			hops[i].TTL = i + 1
		}

		result := &TracerouteResult{
			Destination: "8.8.8.8",
			Hops:        hops,
		}

		assert.Equal(t, count, len(result.Hops))
		if count > 0 {
			assert.Equal(t, 1, result.Hops[0].TTL)
			assert.Equal(t, count, result.Hops[count-1].TTL)
		}
	}
}

func TestTracerouteConfig_DurationConversion(t *testing.T) {
	config := DefaultTracerouteConfig()

	// Verify timeout conversions
	assert.Equal(t, 3000*time.Millisecond, config.Timeout)
	assert.Equal(t, 3*time.Second, config.Timeout)

	// Test custom durations
	customTimeouts := []time.Duration{
		100 * time.Millisecond,
		1 * time.Second,
		10 * time.Second,
		1 * time.Minute,
	}

	for _, timeout := range customTimeouts {
		cfg := &TracerouteConfig{Timeout: timeout}
		assert.Equal(t, timeout, cfg.Timeout)
	}
}
