package metadata

import (
	"os"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	matcher, err := NewLocationMatcher(tmpfile.Name())
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

	matcher, err := NewLocationMatcher(tmpfile.Name())
	require.NoError(t, err)

	loc := matcher.GetLocation("192.168.1.1")
	assert.Equal(t, "unknown", loc)
	assert.Equal(t, 0, matcher.Count())
}

func TestLocationMatcher_NonExistentFile(t *testing.T) {
	_, err := NewLocationMatcher("/nonexistent/file.yaml")
	assert.Error(t, err)
}

func TestLocationMatcher_InvalidYAML(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "locations_invalid_*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpfile.Name())

	_, err = tmpfile.WriteString("invalid: yaml: content: [")
	require.NoError(t, err)
	tmpfile.Close()

	_, err = NewLocationMatcher(tmpfile.Name())
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

	_, err = NewLocationMatcher(tmpfile.Name())
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

	matcher, err := NewLocationMatcher(tmpfile.Name())
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
	matcher := NewEmptyLocationMatcher()

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
	matcher := NewEmptyLocationMatcher()
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
	matcher := NewEmptyLocationMatcher()
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

	matcher := NewEmptyLocationMatcher()
	err = matcher.ParseLocationsFromCSV(tmpfile.Name())
	// Note: CSV parsing is not fully implemented yet
	// This test documents the intended functionality
	assert.Error(t, err) // Expected until CSV parsing is implemented
}

func TestNewEmptyLocationMatcher(t *testing.T) {
	matcher := NewEmptyLocationMatcher()
	require.NotNil(t, matcher)
	assert.Equal(t, 0, matcher.Count())
}

func TestLocationMatcher_GetLocation_InvalidIP(t *testing.T) {
	matcher := NewEmptyLocationMatcher()
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

	matcher, err := NewLocationMatcher(tmpfile.Name())
	require.NoError(t, err)

	assert.Equal(t, "datacenter-a", matcher.GetLocation("192.168.1.50"))
	assert.Equal(t, "datacenter-a", matcher.GetLocation("192.168.2.50"))
	assert.Equal(t, "datacenter-a", matcher.GetLocation("192.168.3.50"))
}
