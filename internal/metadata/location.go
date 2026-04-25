package metadata

import (
	"fmt"
	"net"
	"os"
	"sort"
	"sync"

	"gopkg.in/yaml.v3"
)

// LocationMatcher provides best-match location lookup by IP
type LocationMatcher struct {
	mu       sync.RWMutex
	networks []netWithLocation
}

type netWithLocation struct {
	network  *net.IPNet
	location string
	hostname string
}

type LocationEntry struct {
	Network  string `yaml:"network"`
	Location string `yaml:"location"`
	Hostname string `yaml:"hostname,omitempty"`
}

type LocationsFile struct {
	Locations []LocationEntry `yaml:"locations"`
}

// NewLocationMatcher creates a new location matcher and loads from file
func NewLocationMatcher(path string) (*LocationMatcher, error) {
	m := &LocationMatcher{
		networks: make([]netWithLocation, 0),
	}

	if err := m.Load(path); err != nil {
		return nil, err
	}

	return m, nil
}

// NewEmptyLocationMatcher creates an empty matcher
func NewEmptyLocationMatcher() *LocationMatcher {
	return &LocationMatcher{
		networks: make([]netWithLocation, 0),
	}
}

// Load loads locations from YAML file
func (m *LocationMatcher) Load(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading file: %w", err)
	}

	var file LocationsFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return fmt.Errorf("parsing YAML: %w", err)
	}

	networks := make([]netWithLocation, 0, len(file.Locations))
	for _, entry := range file.Locations {
		_, network, err := net.ParseCIDR(entry.Network)
		if err != nil {
			return fmt.Errorf("parsing network %s: %w", entry.Network, err)
		}

		networks = append(networks, netWithLocation{
			network:  network,
			location: entry.Location,
			hostname: entry.Hostname,
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

// Reload reloads locations from file
func (m *LocationMatcher) Reload(path string) error {
	return m.Load(path)
}

// GetLocation returns the best-match location for an IP
func (m *LocationMatcher) GetLocation(ip string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return "unknown"
	}

	for _, nwl := range m.networks {
		if nwl.network.Contains(parsedIP) {
			return nwl.location
		}
	}

	return "unknown"
}

// GetHostname returns the hostname for an IP if available
func (m *LocationMatcher) GetHostname(ip string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return ""
	}

	for _, nwl := range m.networks {
		if nwl.network.Contains(parsedIP) {
			if nwl.hostname != "" {
				return nwl.hostname
			}
			return nwl.location
		}
	}

	return ""
}

// Count returns the number of loaded networks
func (m *LocationMatcher) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.networks)
}

// ParseLocationsFromCSV parses locations from CSV format (for migration)
func (m *LocationMatcher) ParseLocationsFromCSV(path string) error {
	// TODO: Implement CSV parsing for migration from Python version
	// For now, return error to indicate this is not yet implemented
	return fmt.Errorf("CSV parsing not yet implemented, use YAML format")
}
