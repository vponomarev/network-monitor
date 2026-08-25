package metadata

import (
	"fmt"
	"net"
	"os"
	"sort"
	"sync"

	"go.uber.org/zap"
)

// RoleMatcher provides best-match role lookup by IP
type RoleMatcher struct {
	mu           sync.RWMutex
	networks     []netWithRole
	longestFirst bool
	logger       *zap.Logger
}

type netWithRole struct {
	network *net.IPNet
	role    string
}

type RoleEntry struct {
	Network  string   `yaml:"network,omitempty"`
	Networks []string `yaml:"networks,omitempty"`
	Role     string   `yaml:"role"`
}

type RolesFile struct {
	Roles []RoleEntry `yaml:"roles"`
}

// NewRoleMatcher creates a new role matcher and loads from file
func NewRoleMatcher(path string, logger *zap.Logger) (*RoleMatcher, error) {
	m := &RoleMatcher{
		networks:     make([]netWithRole, 0),
		longestFirst: true,
		logger:       logger.Named("role_matcher"),
	}

	if err := m.Load(path); err != nil {
		return nil, err
	}

	return m, nil
}

// NewEmptyRoleMatcher creates an empty matcher
func NewEmptyRoleMatcher(logger *zap.Logger) *RoleMatcher {
	return &RoleMatcher{
		networks: make([]netWithRole, 0),
		logger:   logger.Named("role_matcher"),
	}
}

// Load loads roles from YAML file
func (m *RoleMatcher) Load(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading file: %w", err)
	}

	var file RolesFile
	if err := decodeYAMLStrict(data, &file); err != nil {
		return fmt.Errorf("parsing YAML: %w", err)
	}

	networks, err := parseRoleEntries(file.Roles)
	if err != nil {
		return err
	}

	// Sort by prefix length (most specific first) - like Python version
	sort.Slice(networks, func(i, j int) bool {
		iLen, _ := networks[i].network.Mask.Size()
		jLen, _ := networks[j].network.Mask.Size()
		return iLen > jLen
	})

	m.mu.Lock()
	m.networks = networks
	m.longestFirst = true
	m.mu.Unlock()

	// Log loaded networks
	m.logger.Info("Roles loaded",
		zap.Int("count", len(networks)),
		zap.String("path", path))

	// Log each network at debug level
	for i, nwr := range networks {
		m.logger.Debug("Loaded network",
			zap.Int("index", i),
			zap.String("network", nwr.network.String()),
			zap.String("role", nwr.role))
	}

	return nil
}

// Reload reloads roles from file
func (m *RoleMatcher) Reload(path string) error {
	return m.Load(path)
}

// GetRole returns the best-match role for an IP (longest prefix match)
func (m *RoleMatcher) GetRole(ip string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return "unknown"
	}

	best := "unknown"
	bestLen := -1
	for _, nwr := range m.networks {
		if nwr.network.Contains(parsedIP) {
			ones, _ := nwr.network.Mask.Size()
			if ones > bestLen {
				best = nwr.role
				bestLen = ones
			}
			if m.longestFirst {
				break
			}
		}
	}
	return best
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
