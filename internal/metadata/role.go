package metadata

import (
	"fmt"
	"net"
	"os"
	"sort"
	"sync"

	"gopkg.in/yaml.v3"
)

// RoleMatcher provides best-match role lookup by IP
type RoleMatcher struct {
	mu       sync.RWMutex
	networks []netWithRole
}

type netWithRole struct {
	network *net.IPNet
	role    string
}

type RoleEntry struct {
	Network string `yaml:"network"`
	Role    string `yaml:"role"`
}

type RolesFile struct {
	Roles []RoleEntry `yaml:"roles"`
}

// NewRoleMatcher creates a new role matcher and loads from file
func NewRoleMatcher(path string) (*RoleMatcher, error) {
	m := &RoleMatcher{
		networks: make([]netWithRole, 0),
	}

	if err := m.Load(path); err != nil {
		return nil, err
	}

	return m, nil
}

// NewEmptyRoleMatcher creates an empty matcher
func NewEmptyRoleMatcher() *RoleMatcher {
	return &RoleMatcher{
		networks: make([]netWithRole, 0),
	}
}

// Load loads roles from YAML file
func (m *RoleMatcher) Load(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading file: %w", err)
	}

	var file RolesFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return fmt.Errorf("parsing YAML: %w", err)
	}

	networks := make([]netWithRole, 0, len(file.Roles))
	for _, entry := range file.Roles {
		_, network, err := net.ParseCIDR(entry.Network)
		if err != nil {
			return fmt.Errorf("parsing network %s: %w", entry.Network, err)
		}

		networks = append(networks, netWithRole{
			network: network,
			role:    entry.Role,
		})
	}

	// Sort by prefix length (most specific first) - like Python version
	sort.Slice(networks, func(i, j int) bool {
		iLen, _ := networks[i].network.Mask.Size()
		jLen, _ := networks[j].network.Mask.Size()
		return iLen > jLen
	})

	m.mu.Lock()
	m.networks = networks
	m.mu.Unlock()

	return nil
}

// Reload reloads roles from file
func (m *RoleMatcher) Reload(path string) error {
	return m.Load(path)
}

// GetRole returns the best-match role for an IP
func (m *RoleMatcher) GetRole(ip string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return "unknown"
	}

	for _, nwr := range m.networks {
		if nwr.network.Contains(parsedIP) {
			return nwr.role
		}
	}

	return "unknown"
}

// Count returns the number of loaded networks
func (m *RoleMatcher) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.networks)
}

// ParseRolesFromCSV parses roles from CSV format (for migration)
func (m *RoleMatcher) ParseRolesFromCSV(path string) error {
	// TODO: Implement CSV parsing for migration from Python version
	// For now, return error to indicate this is not yet implemented
	return fmt.Errorf("CSV parsing not yet implemented, use YAML format")
}
