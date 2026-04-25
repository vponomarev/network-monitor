package metadata

import (
	"os"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRoleMatcher_Load_FromYAML(t *testing.T) {
	// Create temporary YAML file
	tmpfile, err := os.CreateTemp("", "roles_*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpfile.Name())

	yamlContent := `roles:
  - network: 192.168.1.100/32
    role: web-server
  - network: 192.168.1.0/24
    role: web-tier
  - network: 192.168.2.0/24
    role: db-tier
  - network: 192.168.2.50/32
    role: primary-db
`
	_, err = tmpfile.WriteString(yamlContent)
	require.NoError(t, err)
	tmpfile.Close()

	// Load matcher
	matcher, err := NewRoleMatcher(tmpfile.Name())
	require.NoError(t, err)
	require.NotNil(t, matcher)

	// Test best-match (specific /32 wins over /24)
	role := matcher.GetRole("192.168.1.100")
	assert.Equal(t, "web-server", role)

	role = matcher.GetRole("192.168.2.50")
	assert.Equal(t, "primary-db", role)

	// Test /24 match
	role = matcher.GetRole("192.168.1.50")
	assert.Equal(t, "web-tier", role)

	role = matcher.GetRole("192.168.2.100")
	assert.Equal(t, "db-tier", role)

	// Test unknown
	role = matcher.GetRole("10.0.0.1")
	assert.Equal(t, "unknown", role)
}

func TestRoleMatcher_EmptyFile(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "roles_empty_*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpfile.Name())

	_, err = tmpfile.WriteString("roles: []")
	require.NoError(t, err)
	tmpfile.Close()

	matcher, err := NewRoleMatcher(tmpfile.Name())
	require.NoError(t, err)

	role := matcher.GetRole("192.168.1.1")
	assert.Equal(t, "unknown", role)
	assert.Equal(t, 0, matcher.Count())
}

func TestRoleMatcher_NonExistentFile(t *testing.T) {
	_, err := NewRoleMatcher("/nonexistent/file.yaml")
	assert.Error(t, err)
}

func TestRoleMatcher_InvalidYAML(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "roles_invalid_*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpfile.Name())

	_, err = tmpfile.WriteString("invalid: yaml: content: [")
	require.NoError(t, err)
	tmpfile.Close()

	_, err = NewRoleMatcher(tmpfile.Name())
	assert.Error(t, err)
}

func TestRoleMatcher_InvalidNetwork(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "roles_bad_*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpfile.Name())

	yamlContent := `roles:
  - network: invalid-network
    role: test
`
	_, err = tmpfile.WriteString(yamlContent)
	require.NoError(t, err)
	tmpfile.Close()

	_, err = NewRoleMatcher(tmpfile.Name())
	assert.Error(t, err)
}

func TestRoleMatcher_Reload(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "roles_reload_*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpfile.Name())

	// Initial content
	_, err = tmpfile.WriteString("roles:\n  - network: 192.168.1.0/24\n    role: web")
	require.NoError(t, err)
	tmpfile.Close()

	matcher, err := NewRoleMatcher(tmpfile.Name())
	require.NoError(t, err)
	assert.Equal(t, 1, matcher.Count())

	// Update file
	tmpfile, err = os.OpenFile(tmpfile.Name(), os.O_WRONLY|os.O_TRUNC, 0644)
	require.NoError(t, err)
	_, err = tmpfile.WriteString("roles:\n  - network: 10.0.0.0/8\n    role: database")
	require.NoError(t, err)
	tmpfile.Close()

	// Reload
	err = matcher.Reload(tmpfile.Name())
	require.NoError(t, err)
	assert.Equal(t, 1, matcher.Count())

	role := matcher.GetRole("10.5.5.5")
	assert.Equal(t, "database", role)
}

func TestRoleMatcher_BestMatchOrder(t *testing.T) {
	matcher := NewEmptyRoleMatcher()

	// Add in random order
	matcher.networks = []netWithRole{
		{network: mustParseCIDR("10.0.0.0/8"), role: "broad"},
		{network: mustParseCIDR("10.179.64.32/32"), role: "specific"},
		{network: mustParseCIDR("10.179.64.0/24"), role: "medium"},
		{network: mustParseCIDR("10.179.0.0/16"), role: "wide"},
	}

	// Manually sort (as Load does)
	sort.Slice(matcher.networks, func(i, j int) bool {
		iLen, _ := matcher.networks[i].network.Mask.Size()
		jLen, _ := matcher.networks[j].network.Mask.Size()
		return iLen > jLen
	})

	// Most specific should win
	role := matcher.GetRole("10.179.64.32")
	assert.Equal(t, "specific", role)

	// Medium specificity
	role = matcher.GetRole("10.179.64.100")
	assert.Equal(t, "medium", role)

	// Wide
	role = matcher.GetRole("10.179.1.1")
	assert.Equal(t, "wide", role)

	// Broad
	role = matcher.GetRole("10.5.5.5")
	assert.Equal(t, "broad", role)
}

func TestRoleMatcher_EdgeCases(t *testing.T) {
	matcher := NewEmptyRoleMatcher()
	matcher.networks = []netWithRole{
		{network: mustParseCIDR("0.0.0.0/0"), role: "everything"},
	}

	// Should match everything
	role := matcher.GetRole("8.8.8.8")
	assert.Equal(t, "everything", role)

	role = matcher.GetRole("1.1.1.1")
	assert.Equal(t, "everything", role)
}

func TestRoleMatcher_Concurrent(t *testing.T) {
	matcher := NewEmptyRoleMatcher()
	matcher.networks = []netWithRole{
		{network: mustParseCIDR("192.168.1.0/24"), role: "office"},
	}

	done := make(chan bool, 100)

	// Concurrent reads
	for i := 0; i < 100; i++ {
		go func() {
			_ = matcher.GetRole("192.168.1.1")
			_ = matcher.GetRole("192.168.1.2")
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

func TestRoleMatcher_Load_FromCSV(t *testing.T) {
	// Create temporary CSV file
	tmpfile, err := os.CreateTemp("", "roles_*.csv")
	require.NoError(t, err)
	defer os.Remove(tmpfile.Name())

	csvContent := `IP,Role
10.179.64.32/32,s3-dwh05
10.179.65.31/32,dwh-lb
10.179.64.0/22,DWH
`
	_, err = tmpfile.WriteString(csvContent)
	require.NoError(t, err)
	tmpfile.Close()

	matcher := NewEmptyRoleMatcher()
	err = matcher.ParseRolesFromCSV(tmpfile.Name())
	// Note: CSV parsing is not fully implemented yet
	// This test documents the intended functionality
	assert.Error(t, err) // Expected until CSV parsing is implemented
}

func TestNewEmptyRoleMatcher(t *testing.T) {
	matcher := NewEmptyRoleMatcher()
	require.NotNil(t, matcher)
	assert.Equal(t, 0, matcher.Count())
}

func TestRoleMatcher_GetRole_InvalidIP(t *testing.T) {
	matcher := NewEmptyRoleMatcher()
	matcher.networks = []netWithRole{
		{network: mustParseCIDR("10.0.0.0/8"), role: "test"},
	}

	role := matcher.GetRole("not-an-ip")
	assert.Equal(t, "unknown", role)

	role = matcher.GetRole("")
	assert.Equal(t, "unknown", role)

	role = matcher.GetRole("256.256.256.256")
	assert.Equal(t, "unknown", role)
}

func TestRoleMatcher_MultipleNetworksSameRole(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "roles_multi_*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpfile.Name())

	yamlContent := `roles:
  - network: 192.168.1.0/24
    role: web-server
  - network: 192.168.2.0/24
    role: web-server
  - network: 192.168.3.0/24
    role: web-server
`
	_, err = tmpfile.WriteString(yamlContent)
	require.NoError(t, err)
	tmpfile.Close()

	matcher, err := NewRoleMatcher(tmpfile.Name())
	require.NoError(t, err)

	assert.Equal(t, "web-server", matcher.GetRole("192.168.1.50"))
	assert.Equal(t, "web-server", matcher.GetRole("192.168.2.50"))
	assert.Equal(t, "web-server", matcher.GetRole("192.168.3.50"))
}

func TestRoleMatcher_HierarchicalRoles(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "roles_hier_*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpfile.Name())

	yamlContent := `roles:
  - network: 192.168.1.100/32
    role: primary-web-01
  - network: 192.168.1.0/24
    role: web-tier
  - network: 192.168.0.0/16
    role: production
`
	_, err = tmpfile.WriteString(yamlContent)
	require.NoError(t, err)
	tmpfile.Close()

	matcher, err := NewRoleMatcher(tmpfile.Name())
	require.NoError(t, err)

	// Most specific wins
	assert.Equal(t, "primary-web-01", matcher.GetRole("192.168.1.100"))
	assert.Equal(t, "web-tier", matcher.GetRole("192.168.1.50"))
	assert.Equal(t, "production", matcher.GetRole("192.168.100.50"))
}
