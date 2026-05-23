package metadata

import (
	"net"
	"os"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// mustParseCIDR parses a CIDR string and panics on error (for tests only)
func mustParseCIDR(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return n
}

// newTestLogger creates a test logger
func newTestLogger(t *testing.T) *zap.Logger {
	logger, err := zap.NewDevelopment()
	if err != nil {
		t.Fatal(err)
	}
	return logger
}

func TestLocationMatcher_Load_FromYAML(t *testing.T) {
	// Create temporary YAML file
	tmpfile, err := os.CreateTemp("", "locations_*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpfile.Name())

	yamlContent := `locations:
  - network: 192.168.1.0/24
    location: office-ny
  - network: 192.168.2.0/24
    location: office-la
  - network: 192.168.1.100/32
    location: server-room
    hostname: web-server-01
`
	_, err = tmpfile.WriteString(yamlContent)
	require.NoError(t, err)
	tmpfile.Close()

	// Load matcher
	matcher, err := NewLocationMatcher(tmpfile.Name(), newTestLogger(t))
	require.NoError(t, err)
	require.NotNil(t, matcher)

	// Test best-match (specific /32 wins over /24)
	loc := matcher.GetLocation("192.168.1.100")
	assert.Equal(t, "server-room", loc)

	// Test /24 match
	loc = matcher.GetLocation("192.168.1.50")
	assert.Equal(t, "office-ny", loc)

	// Test another /24
	loc = matcher.GetLocation("192.168.2.10")
	assert.Equal(t, "office-la", loc)

	// Test unknown
	loc = matcher.GetLocation("10.0.0.1")
	assert.Equal(t, "unknown", loc)

	// Test hostname
	hostname := matcher.GetHostname("192.168.1.100")
	assert.Equal(t, "web-server-01", hostname)

	// Test hostname fallback to location
	hostname = matcher.GetHostname("192.168.1.50")
	assert.Equal(t, "office-ny", hostname)
}

func TestLocationMatcher_EmptyFile(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "locations_empty_*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpfile.Name())

	_, err = tmpfile.WriteString("locations: []")
	require.NoError(t, err)
	tmpfile.Close()

	matcher, err := NewLocationMatcher(tmpfile.Name(), newTestLogger(t))
	require.NoError(t, err)

	loc := matcher.GetLocation("192.168.1.1")
	assert.Equal(t, "unknown", loc)
	assert.Equal(t, 0, matcher.Count())
}

func TestLocationMatcher_NonExistentFile(t *testing.T) {
	_, err := NewLocationMatcher("/nonexistent/file.yaml", newTestLogger(t))
	assert.Error(t, err)
}

func TestLocationMatcher_InvalidYAML(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "locations_invalid_*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpfile.Name())

	_, err = tmpfile.WriteString("invalid: yaml: content: [")
	require.NoError(t, err)
	tmpfile.Close()

	_, err = NewLocationMatcher(tmpfile.Name(), newTestLogger(t))
	assert.Error(t, err)
}

func TestLocationMatcher_InvalidNetwork(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "locations_bad_*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpfile.Name())

	yamlContent := `locations:
  - network: invalid-network
    location: test
`
	_, err = tmpfile.WriteString(yamlContent)
	require.NoError(t, err)
	tmpfile.Close()

	_, err = NewLocationMatcher(tmpfile.Name(), newTestLogger(t))
	assert.Error(t, err)
}

func TestLocationMatcher_Reload(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "locations_reload_*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpfile.Name())

	// Initial content
	_, err = tmpfile.WriteString("locations:\n  - network: 192.168.1.0/24\n    location: office-ny")
	require.NoError(t, err)
	tmpfile.Close()

	matcher, err := NewLocationMatcher(tmpfile.Name(), newTestLogger(t))
	require.NoError(t, err)
	assert.Equal(t, 1, matcher.Count())

	// Update file
	tmpfile, err = os.OpenFile(tmpfile.Name(), os.O_WRONLY|os.O_TRUNC, 0644)
	require.NoError(t, err)
	_, err = tmpfile.WriteString("locations:\n  - network: 10.0.0.0/8\n    location: datacenter")
	require.NoError(t, err)
	tmpfile.Close()

	// Reload
	err = matcher.Reload(tmpfile.Name())
	require.NoError(t, err)
	assert.Equal(t, 1, matcher.Count())

	loc := matcher.GetLocation("10.5.5.5")
	assert.Equal(t, "datacenter", loc)
}

func TestLocationMatcher_BestMatchOrder(t *testing.T) {
	matcher := NewEmptyLocationMatcher(newTestLogger(t))

	// Add in random order
	matcher.networks = []netWithLocation{
		{network: mustParseCIDR("10.0.0.0/8"), location: "broad"},
		{network: mustParseCIDR("10.179.64.32/32"), location: "specific"},
		{network: mustParseCIDR("10.179.64.0/24"), location: "medium"},
		{network: mustParseCIDR("10.179.0.0/16"), location: "wide"},
	}

	// Manually sort (as Load does)
	sort.Slice(matcher.networks, func(i, j int) bool {
		iLen, _ := matcher.networks[i].network.Mask.Size()
		jLen, _ := matcher.networks[j].network.Mask.Size()
		return iLen > jLen
	})

	// Most specific should win
	loc := matcher.GetLocation("10.179.64.32")
	assert.Equal(t, "specific", loc)

	// Medium specificity
	loc = matcher.GetLocation("10.179.64.100")
	assert.Equal(t, "medium", loc)

	// Wide
	loc = matcher.GetLocation("10.179.1.1")
	assert.Equal(t, "wide", loc)

	// Broad
	loc = matcher.GetLocation("10.5.5.5")
	assert.Equal(t, "broad", loc)
}

func TestLocationMatcher_EdgeCases(t *testing.T) {
	matcher := NewEmptyLocationMatcher(newTestLogger(t))
	matcher.networks = []netWithLocation{
		{network: mustParseCIDR("0.0.0.0/0"), location: "everywhere"},
	}

	// Should match everything
	loc := matcher.GetLocation("8.8.8.8")
	assert.Equal(t, "everywhere", loc)

	loc = matcher.GetLocation("1.1.1.1")
	assert.Equal(t, "everywhere", loc)
}

func TestLocationMatcher_Concurrent(t *testing.T) {
	matcher := NewEmptyLocationMatcher(newTestLogger(t))
	matcher.networks = []netWithLocation{
		{network: mustParseCIDR("192.168.1.0/24"), location: "office"},
	}

	done := make(chan bool, 100)

	// Concurrent reads
	for i := 0; i < 100; i++ {
		go func() {
			_ = matcher.GetLocation("192.168.1.1")
			_ = matcher.GetLocation("192.168.1.2")
			_ = matcher.Count()
			done <- true
		}()
	}

	for i := 0; i < 100; i++ {
		<-done
	}

	// Should not panic
	assert.Equal(t, 1, matcher.Count())
}

func TestLocationMatcher_Load_FromCSV(t *testing.T) {
	// Create temporary CSV file
	tmpfile, err := os.CreateTemp("", "locations_*.csv")
	require.NoError(t, err)
	defer os.Remove(tmpfile.Name())

	csvContent := `IP,Location
10.146.22.0/24,IX-M4-SM3
10.179.64.0/22,IX-M5-SM13
10.179.65.31/32,IX-M5-SM13
`
	_, err = tmpfile.WriteString(csvContent)
	require.NoError(t, err)
	tmpfile.Close()

	matcher := NewEmptyLocationMatcher(newTestLogger(t))
	err = matcher.ParseLocationsFromCSV(tmpfile.Name())
	// Note: CSV parsing is not fully implemented yet
	// This test documents the intended functionality
	assert.Error(t, err) // Expected until CSV parsing is implemented
}

func TestNewEmptyLocationMatcher(t *testing.T) {
	matcher := NewEmptyLocationMatcher(newTestLogger(t))
	require.NotNil(t, matcher)
	assert.Equal(t, 0, matcher.Count())
}

func TestLocationMatcher_GetVrf(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "locations_vrf_*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpfile.Name())

	yamlContent := `locations:
  - network: 10.179.64.0/24
    location: IX-M5
    vrf: mgmt-vrf
  - network: 10.179.65.0/24
    location: IX-M3
    # vrf не указан
  - network: 10.198.8.0/24
    location: DS-402
    vrf: default-vrf
`
	_, err = tmpfile.WriteString(yamlContent)
	require.NoError(t, err)
	tmpfile.Close()

	matcher, err := NewLocationMatcher(tmpfile.Name(), newTestLogger(t))
	require.NoError(t, err)

	// Test VRF match
	vrf := matcher.GetVrf("10.179.64.100")
	assert.Equal(t, "mgmt-vrf", vrf)

	// Test VRF not specified - should return "unknown"
	vrf = matcher.GetVrf("10.179.65.50")
	assert.Equal(t, "unknown", vrf)

	// Test another VRF match
	vrf = matcher.GetVrf("10.198.8.10")
	assert.Equal(t, "default-vrf", vrf)

	// Test unknown IP
	vrf = matcher.GetVrf("8.8.8.8")
	assert.Equal(t, "unknown", vrf)

	// Test invalid IP
	vrf = matcher.GetVrf("invalid")
	assert.Equal(t, "unknown", vrf)
}

func TestLocationMatcher_GetVrf_EmptyFile(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "locations_empty_vrf_*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpfile.Name())

	_, err = tmpfile.WriteString("locations: []")
	require.NoError(t, err)
	tmpfile.Close()

	matcher, err := NewLocationMatcher(tmpfile.Name(), newTestLogger(t))
	require.NoError(t, err)

	vrf := matcher.GetVrf("10.0.0.1")
	assert.Equal(t, "unknown", vrf)
}

func TestLocationMatcher_GetVrf_BestMatch(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "locations_vrf_best_*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpfile.Name())

	yamlContent := `locations:
  - network: 10.179.64.0/22
    location: IX-M5-SM13
    vrf: shared-vrf
  - network: 10.179.64.32/32
    location: IX-M5-SM13-specific
    vrf: mgmt-vrf
`
	_, err = tmpfile.WriteString(yamlContent)
	require.NoError(t, err)
	tmpfile.Close()

	matcher, err := NewLocationMatcher(tmpfile.Name(), newTestLogger(t))
	require.NoError(t, err)

	// Most specific /32 should win
	vrf := matcher.GetVrf("10.179.64.32")
	assert.Equal(t, "mgmt-vrf", vrf)

	// /22 should match for other IPs in range
	vrf = matcher.GetVrf("10.179.64.100")
	assert.Equal(t, "shared-vrf", vrf)
}

func TestLocationMatcher_GetLocation_InvalidIP(t *testing.T) {
	matcher := NewEmptyLocationMatcher(newTestLogger(t))
	matcher.networks = []netWithLocation{
		{network: mustParseCIDR("10.0.0.0/8"), location: "test"},
	}

	loc := matcher.GetLocation("not-an-ip")
	assert.Equal(t, "unknown", loc)

	loc = matcher.GetLocation("")
	assert.Equal(t, "unknown", loc)

	loc = matcher.GetLocation("256.256.256.256")
	assert.Equal(t, "unknown", loc)
}

func TestLocationMatcher_MultipleNetworksSameLocation(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "locations_multi_*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpfile.Name())

	yamlContent := `locations:
  - network: 192.168.1.0/24
    location: datacenter-a
  - network: 192.168.2.0/24
    location: datacenter-a
  - network: 192.168.3.0/24
    location: datacenter-a
`
	_, err = tmpfile.WriteString(yamlContent)
	require.NoError(t, err)
	tmpfile.Close()

	matcher, err := NewLocationMatcher(tmpfile.Name(), newTestLogger(t))
	require.NoError(t, err)

	assert.Equal(t, "datacenter-a", matcher.GetLocation("192.168.1.50"))
	assert.Equal(t, "datacenter-a", matcher.GetLocation("192.168.2.50"))
	assert.Equal(t, "datacenter-a", matcher.GetLocation("192.168.3.50"))
}

// TestLocationMatcher_UserSubnets tests the specific subnets from the user's configuration
func TestLocationMatcher_UserSubnets(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "locations_user_*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpfile.Name())

	// User's actual configuration
	yamlContent := `locations:
  - network: 10.179.68.0/22
    location: IX-M5.42
  - network: 10.198.0.0/28
    location: DS-402
`
	_, err = tmpfile.WriteString(yamlContent)
	require.NoError(t, err)
	tmpfile.Close()

	matcher, err := NewLocationMatcher(tmpfile.Name(), newTestLogger(t))
	require.NoError(t, err)

	// Test 10.179.68.0/22 = 10.179.68.0 - 10.179.71.255
	t.Run("IX-M5.42 subnet", func(t *testing.T) {
		// All these IPs should match 10.179.68.0/22
		testIPs := []string{
			"10.179.68.13",
			"10.179.68.138",
			"10.179.68.139",
			"10.179.68.140",
			"10.179.68.142",
			"10.179.68.143",
			"10.179.68.144",
			"10.179.68.145",
			"10.179.68.146",
			"10.179.68.147",
			"10.179.68.148",
			"10.179.68.149",
			"10.179.68.254",
			"10.179.69.1",
			"10.179.70.1",
			"10.179.71.254",
		}

		for _, ip := range testIPs {
			t.Run(ip, func(t *testing.T) {
				loc := matcher.GetLocation(ip)
				assert.Equal(t, "IX-M5.42", loc, "IP %s should match 10.179.68.0/22", ip)
			})
		}
	})

	// Test 10.198.0.0/28 = 10.198.0.0 - 10.198.0.15
	t.Run("DS-402 subnet", func(t *testing.T) {
		// These IPs should match 10.198.0.0/28
		matchingIPs := []string{
			"10.198.0.1",
			"10.198.0.14",
			"10.198.0.15",
		}

		for _, ip := range matchingIPs {
			t.Run(ip, func(t *testing.T) {
				loc := matcher.GetLocation(ip)
				assert.Equal(t, "DS-402", loc, "IP %s should match 10.198.0.0/28", ip)
			})
		}

		// These IPs should NOT match (they're in 10.198.8.x, not 10.198.0.x)
		nonMatchingIPs := []string{
			"10.198.8.63",
			"10.198.8.1",
			"10.198.8.254",
			"10.198.1.1",
		}

		for _, ip := range nonMatchingIPs {
			t.Run(ip, func(t *testing.T) {
				loc := matcher.GetLocation(ip)
				assert.Equal(t, "unknown", loc, "IP %s should NOT match 10.198.0.0/28", ip)
			})
		}
	})

	// Test IPs outside all configured subnets
	t.Run("unknown locations", func(t *testing.T) {
		unknownIPs := []string{
			"10.118.52.38",
			"10.181.212.177",
			"10.208.200.4",
			"8.8.8.8",
			"1.1.1.1",
		}

		for _, ip := range unknownIPs {
			t.Run(ip, func(t *testing.T) {
				loc := matcher.GetLocation(ip)
				assert.Equal(t, "unknown", loc, "IP %s should be unknown", ip)
			})
		}
	})
}

// TestLocationMatcher_CIDRRanges verifies correct CIDR range calculations
func TestLocationMatcher_CIDRRanges(t *testing.T) {
	tests := []struct {
		name           string
		cidr           string
		shouldMatch    []string
		shouldNotMatch []string
	}{
		{
			name: "/22 range",
			cidr: "10.179.68.0/22",
			shouldMatch: []string{
				"10.179.68.0",
				"10.179.68.13",
				"10.179.68.255",
				"10.179.69.1",
				"10.179.70.1",
				"10.179.71.255",
			},
			shouldNotMatch: []string{
				"10.179.67.255",
				"10.179.72.0",
				"10.179.64.1",
			},
		},
		{
			name: "/28 range",
			cidr: "10.198.0.0/28",
			shouldMatch: []string{
				"10.198.0.0",
				"10.198.0.1",
				"10.198.0.14",
				"10.198.0.15",
			},
			shouldNotMatch: []string{
				"10.198.0.16",
				"10.198.0.31",
				"10.198.8.63",
			},
		},
		{
			name: "/24 range",
			cidr: "10.179.68.0/24",
			shouldMatch: []string{
				"10.179.68.0",
				"10.179.68.128",
				"10.179.68.255",
			},
			shouldNotMatch: []string{
				"10.179.67.255",
				"10.179.69.0",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpfile, err := os.CreateTemp("", "locations_cidr_*.yaml")
			require.NoError(t, err)
			defer os.Remove(tmpfile.Name())

			yamlContent := "locations:\n  - network: " + tt.cidr + "\n    location: test-location\n"
			_, err = tmpfile.WriteString(yamlContent)
			require.NoError(t, err)
			tmpfile.Close()

			matcher, err := NewLocationMatcher(tmpfile.Name(), newTestLogger(t))
			require.NoError(t, err)

			for _, ip := range tt.shouldMatch {
				loc := matcher.GetLocation(ip)
				assert.Equal(t, "test-location", loc, "IP %s should match %s", ip, tt.cidr)
			}

			for _, ip := range tt.shouldNotMatch {
				loc := matcher.GetLocation(ip)
				assert.Equal(t, "unknown", loc, "IP %s should NOT match %s", ip, tt.cidr)
			}
		})
	}
}
