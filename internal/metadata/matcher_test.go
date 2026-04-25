package metadata

import (
	"net"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLocationMatcher_BestMatch(t *testing.T) {
	// Create test data similar to Python version
	matcher := NewEmptyLocationMatcher()
	matcher.networks = []netWithLocation{
		{network: mustParseCIDR("10.179.64.0/22"), location: "DWH"},
		{network: mustParseCIDR("10.179.64.32/32"), location: "IX-M5-SM13"},
		{network: mustParseCIDR("10.179.65.31/32"), location: "IX-M5-SM13"},
	}

	// Sort to ensure best-match order (as Load does)
	sort.Slice(matcher.networks, func(i, j int) bool {
		iLen, _ := matcher.networks[i].network.Mask.Size()
		jLen, _ := matcher.networks[j].network.Mask.Size()
		return iLen > jLen
	})

	// Test best-match: /32 should win over /22
	location := matcher.GetLocation("10.179.64.32")
	assert.Equal(t, "IX-M5-SM13", location)

	// Test /22 match
	location = matcher.GetLocation("10.179.64.100")
	assert.Equal(t, "DWH", location)

	// Test unknown
	location = matcher.GetLocation("192.168.1.1")
	assert.Equal(t, "unknown", location)
}

func TestRoleMatcher_BestMatch(t *testing.T) {
	matcher := NewEmptyRoleMatcher()
	matcher.networks = []netWithRole{
		{network: mustParseCIDR("10.179.64.0/22"), role: "dwh-storage"},
		{network: mustParseCIDR("10.179.64.32/32"), role: "s3-dwh05"},
		{network: mustParseCIDR("10.179.65.31/32"), role: "dwh-lb"},
	}

	// Sort to ensure best-match order (as Load does)
	sort.Slice(matcher.networks, func(i, j int) bool {
		iLen, _ := matcher.networks[i].network.Mask.Size()
		jLen, _ := matcher.networks[j].network.Mask.Size()
		return iLen > jLen
	})

	// Test best-match: /32 should win over /22
	role := matcher.GetRole("10.179.64.32")
	assert.Equal(t, "s3-dwh05", role)

	// Test /22 match
	role = matcher.GetRole("10.179.64.100")
	assert.Equal(t, "dwh-storage", role)

	// Test unknown
	role = matcher.GetRole("192.168.1.1")
	assert.Equal(t, "unknown", role)
}

func TestLocationMatcher_Count(t *testing.T) {
	matcher := NewEmptyLocationMatcher()
	assert.Equal(t, 0, matcher.Count())

	matcher.networks = []netWithLocation{
		{network: mustParseCIDR("10.0.0.0/8"), location: "test"},
	}
	assert.Equal(t, 1, matcher.Count())
}

func TestRoleMatcher_Count(t *testing.T) {
	matcher := NewEmptyRoleMatcher()
	assert.Equal(t, 0, matcher.Count())

	matcher.networks = []netWithRole{
		{network: mustParseCIDR("10.0.0.0/8"), role: "test"},
	}
	assert.Equal(t, 1, matcher.Count())
}

func TestLocationMatcher_GetHostname(t *testing.T) {
	matcher := NewEmptyLocationMatcher()
	matcher.networks = []netWithLocation{
		{network: mustParseCIDR("10.179.65.31/32"), location: "IX-M5-SM13", hostname: "dwh-lb-01"},
		{network: mustParseCIDR("10.179.64.0/22"), location: "DWH"},
	}

	// Test with hostname
	hostname := matcher.GetHostname("10.179.65.31")
	assert.Equal(t, "dwh-lb-01", hostname)

	// Test without hostname (returns location)
	hostname = matcher.GetHostname("10.179.64.32")
	assert.Equal(t, "DWH", hostname)

	// Test unknown
	hostname = matcher.GetHostname("192.168.1.1")
	assert.Equal(t, "", hostname)
}

func TestLocationMatcher_InvalidIP(t *testing.T) {
	matcher := NewEmptyLocationMatcher()
	matcher.networks = []netWithLocation{
		{network: mustParseCIDR("10.0.0.0/8"), location: "test"},
	}

	location := matcher.GetLocation("invalid-ip")
	assert.Equal(t, "unknown", location)
}

func TestRoleMatcher_InvalidIP(t *testing.T) {
	matcher := NewEmptyRoleMatcher()
	matcher.networks = []netWithRole{
		{network: mustParseCIDR("10.0.0.0/8"), role: "test"},
	}

	role := matcher.GetRole("invalid-ip")
	assert.Equal(t, "unknown", role)
}

// Helper function for tests
func mustParseCIDR(s string) *net.IPNet {
	_, network, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return network
}

// Test that sorting works correctly (most specific first)
func TestLocationMatcher_Sorting(t *testing.T) {
	matcher := NewEmptyLocationMatcher()
	matcher.networks = []netWithLocation{
		{network: mustParseCIDR("10.0.0.0/8"), location: "broad"},
		{network: mustParseCIDR("10.179.64.32/32"), location: "specific"},
		{network: mustParseCIDR("10.179.64.0/24"), location: "medium"},
	}

	// Manually sort to verify the logic
	sort.Slice(matcher.networks, func(i, j int) bool {
		iLen, _ := matcher.networks[i].network.Mask.Size()
		jLen, _ := matcher.networks[j].network.Mask.Size()
		return iLen > jLen
	})

	// Verify order: /32, /24, /8
	assert.Equal(t, "specific", matcher.networks[0].location)
	assert.Equal(t, "medium", matcher.networks[1].location)
	assert.Equal(t, "broad", matcher.networks[2].location)
}
