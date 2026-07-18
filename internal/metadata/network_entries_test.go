package metadata

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeMetadataFile(t *testing.T, content string) string {
	t.Helper()
	file, err := os.CreateTemp("", "metadata_networks_*.yaml")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Remove(file.Name()) })
	_, err = file.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, file.Close())
	return file.Name()
}

func TestRoleMatcherGroupedNetworks(t *testing.T) {
	path := writeMetadataFile(t, `roles:
  - role: load-balancer
    networks:
      - 10.10.10.10/32
      - 10.10.10.11/32
      - 10.12.10.12/32
      - 10.10.10.10/32
  - network: 10.20.0.0/16
    role: database
`)

	matcher, err := NewRoleMatcher(path, newTestRoleLogger(t))
	require.NoError(t, err)
	assert.Equal(t, 4, matcher.Count(), "identical CIDRs should be deduplicated")
	assert.Equal(t, "load-balancer", matcher.GetRole("10.10.10.11"))
	assert.Equal(t, "load-balancer", matcher.GetRole("10.12.10.12"))
	assert.Equal(t, "database", matcher.GetRole("10.20.5.8"))
}

func TestLocationMatcherGroupedNetworks(t *testing.T) {
	path := writeMetadataFile(t, `locations:
  - location: datacenter-a
    vrf: production
    networks:
      - 10.32.0.0/20
      - 10.40.0.0/23
      - 10.50.10.0/24
  - network: 10.50.10.42/32
    location: datacenter-b
    vrf: management
`)

	matcher, err := NewLocationMatcher(path, newTestLogger(t))
	require.NoError(t, err)
	assert.Equal(t, 4, matcher.Count())
	assert.Equal(t, "datacenter-a", matcher.GetLocation("10.32.15.254"))
	assert.Equal(t, "datacenter-a", matcher.GetLocation("10.40.1.10"))
	assert.Equal(t, "production", matcher.GetVrf("10.50.10.8"))
	assert.Equal(t, "datacenter-b", matcher.GetLocation("10.50.10.42"), "longest prefix must still win")
	assert.Equal(t, "management", matcher.GetVrf("10.50.10.42"))
}

func TestGroupedNetworkValidation(t *testing.T) {
	tests := []struct {
		name      string
		validator Validator
		data      string
		contains  string
	}{
		{
			name:      "role fields are mutually exclusive",
			validator: RolesValidator,
			data:      "roles:\n  - network: 10.0.0.1/32\n    networks: [10.0.0.2/32]\n    role: web",
			contains:  "mutually exclusive",
		},
		{
			name:      "empty role list",
			validator: RolesValidator,
			data:      "roles:\n  - networks: []\n    role: web",
			contains:  "network or networks is required",
		},
		{
			name:      "empty list item",
			validator: LocationsValidator,
			data:      "locations:\n  - networks: [10.0.0.0/24, '']\n    location: dc1",
			contains:  "must not be empty",
		},
		{
			name:      "invalid grouped CIDR",
			validator: LocationsValidator,
			data:      "locations:\n  - networks: [not-a-cidr]\n    location: dc1",
			contains:  "invalid network",
		},
		{
			name:      "conflicting role",
			validator: RolesValidator,
			data:      "roles:\n  - network: 10.0.0.1/24\n    role: web\n  - network: 10.0.0.0/24\n    role: db",
			contains:  "conflicts with role",
		},
		{
			name:      "conflicting location attributes",
			validator: LocationsValidator,
			data:      "locations:\n  - network: 10.0.0.0/24\n    location: dc1\n  - network: 10.0.0.0/24\n    location: dc2",
			contains:  "conflicting location attributes",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.validator([]byte(test.data))
			require.Error(t, err)
			assert.Contains(t, err.Error(), test.contains)
		})
	}
}

func TestRoleReloadRejectsConflictAndPreservesActiveData(t *testing.T) {
	path := writeMetadataFile(t, "roles:\n  - network: 10.0.0.1/32\n    role: web\n")
	matcher, err := NewRoleMatcher(path, newTestRoleLogger(t))
	require.NoError(t, err)

	err = os.WriteFile(path, []byte("roles:\n  - network: 10.0.0.1/32\n    role: web\n  - network: 10.0.0.1/32\n    role: db\n"), 0600)
	require.NoError(t, err)
	require.Error(t, matcher.Reload(path))
	assert.Equal(t, "web", matcher.GetRole("10.0.0.1"))
}

func TestLocationReloadRejectsConflictAndPreservesActiveData(t *testing.T) {
	path := writeMetadataFile(t, "locations:\n  - network: 10.0.0.0/24\n    location: dc1\n")
	matcher, err := NewLocationMatcher(path, newTestLogger(t))
	require.NoError(t, err)

	err = os.WriteFile(path, []byte("locations:\n  - network: 10.0.0.0/24\n    location: dc1\n  - network: 10.0.0.0/24\n    location: dc2\n"), 0600)
	require.NoError(t, err)
	require.Error(t, matcher.Reload(path))
	assert.Equal(t, "dc1", matcher.GetLocation("10.0.0.10"))
}

func TestGroupedMetadataExampleFiles(t *testing.T) {
	tests := []struct {
		path      string
		validator Validator
	}{
		{path: "../../configs/roles.example.yaml", validator: RolesValidator},
		{path: "../../configs/locations.example.yaml", validator: LocationsValidator},
	}

	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			data, err := os.ReadFile(test.path)
			require.NoError(t, err)
			require.NoError(t, test.validator(data))
		})
	}
}
