package discovery

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPath_PathID(t *testing.T) {
	path := &Path{
		SrcIP: net.ParseIP("192.168.1.1"),
		DstIP: net.ParseIP("192.168.1.2"),
	}

	id := path.PathID()
	assert.Equal(t, "path-192.168.1.1-192.168.1.2", id)
}

func TestPath_TotalLoss(t *testing.T) {
	tests := []struct {
		name     string
		hops     []Hop
		expected float64
	}{
		{
			name:     "no hops",
			hops:     []Hop{},
			expected: 0,
		},
		{
			name: "no loss",
			hops: []Hop{
				{TTL: 1, Lost: false},
				{TTL: 2, Lost: false},
				{TTL: 3, Lost: false},
			},
			expected: 0,
		},
		{
			name: "50% loss",
			hops: []Hop{
				{TTL: 1, Lost: false},
				{TTL: 2, Lost: true},
				{TTL: 3, Lost: true},
				{TTL: 4, Lost: false},
			},
			expected: 50,
		},
		{
			name: "100% loss",
			hops: []Hop{
				{TTL: 1, Lost: true},
				{TTL: 2, Lost: true},
			},
			expected: 100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := &Path{Hops: tt.hops}
			assert.Equal(t, tt.expected, path.TotalLoss())
		})
	}
}

func TestPath_AvgRTT(t *testing.T) {
	tests := []struct {
		name     string
		hops     []Hop
		expected time.Duration
	}{
		{
			name:     "no hops",
			hops:     []Hop{},
			expected: 0,
		},
		{
			name: "all lost",
			hops: []Hop{
				{TTL: 1, Lost: true, RTT: 10 * time.Millisecond},
				{TTL: 2, Lost: true, RTT: 20 * time.Millisecond},
			},
			expected: 0,
		},
		{
			name: "mixed",
			hops: []Hop{
				{TTL: 1, Lost: false, RTT: 10 * time.Millisecond},
				{TTL: 2, Lost: true, RTT: 0},
				{TTL: 3, Lost: false, RTT: 20 * time.Millisecond},
			},
			expected: 15 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := &Path{Hops: tt.hops}
			assert.Equal(t, tt.expected, path.AvgRTT())
		})
	}
}

func TestFindBottleneck(t *testing.T) {
	tests := []struct {
		name     string
		path     *Path
		wantNil  bool
		wantHop  int
		wantLoss float64
	}{
		{
			name:    "nil path",
			path:    nil,
			wantNil: true,
		},
		{
			name:    "empty hops",
			path:    &Path{Hops: []Hop{}},
			wantNil: true,
		},
		{
			name: "no loss",
			path: &Path{
				Hops: []Hop{
					{TTL: 1, Lost: false},
					{TTL: 2, Lost: false},
				},
			},
			wantNil: true,
		},
		{
			name: "bottleneck at hop 2",
			path: &Path{
				Hops: []Hop{
					{TTL: 1, Lost: false, IP: net.ParseIP("10.0.0.1")},
					{TTL: 2, Lost: true, IP: net.ParseIP("10.0.0.2")},
					{TTL: 3, Lost: true, IP: net.ParseIP("10.0.0.3")},
				},
			},
			wantNil:  false,
			wantHop:  3,     // The bottleneck is at the last lost hop (highest loss %)
			wantLoss: 66.67, // 2/3 hops lost
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FindBottleneck(tt.path)
			if tt.wantNil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				assert.Equal(t, tt.wantHop, result.HopTTL)
				assert.InDelta(t, tt.wantLoss, result.LossPercent, 0.01)
			}
		})
	}
}

func TestDefaultTracerouter_Run(t *testing.T) {
	tracerouter := NewDefaultTracerouter()
	ctx := context.Background()

	// Test with invalid destination
	path, err := tracerouter.Run(ctx, "192.168.1.1", "invalid-ip")
	assert.Error(t, err)
	assert.Nil(t, path)

	// Test with valid destination (returns placeholder on non-Linux)
	path, err = tracerouter.Run(ctx, "192.168.1.1", "8.8.8.8")
	require.NoError(t, err)
	require.NotNil(t, path)
	assert.Equal(t, "8.8.8.8", path.DstIP.String())
}

func TestDefaultTracerouter_RunWithTimeout(t *testing.T) {
	tracerouter := NewDefaultTracerouter()
	ctx := context.Background()

	// Test with custom timeout
	path, err := tracerouter.RunWithTimeout(ctx, "192.168.1.1", "8.8.8.8", 5*time.Second)
	require.NoError(t, err)
	require.NotNil(t, path)
	assert.Equal(t, 10*time.Minute, path.TTL)
}

func TestPath_Discovered(t *testing.T) {
	before := time.Now()
	path := &Path{
		SrcIP: net.ParseIP("192.168.1.1"),
		DstIP: net.ParseIP("192.168.1.2"),
		Hops:  []Hop{},
	}
	// Simulate discovery
	path.Discovered = time.Now()
	after := time.Now()

	assert.True(t, !path.Discovered.IsZero())
	assert.True(t, path.Discovered.After(before) || path.Discovered.Equal(before))
	assert.True(t, path.Discovered.Before(after) || path.Discovered.Equal(after))
}
