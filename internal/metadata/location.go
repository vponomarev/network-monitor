package metadata

import (
	"fmt"
	"net"
	"os"
	"sort"
	"sync"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// LocationMatcher provides best-match location lookup by IP
type LocationMatcher struct {
	mu       sync.RWMutex
	networks []netWithLocation
	logger   *zap.Logger
}

type netWithLocation struct {
	network  *net.IPNet
	location string
	hostname string
	vrf      string
}

type LocationEntry struct {
	Network  string `yaml:"network"`
	Location string `yaml:"location"`
	Hostname string `yaml:"hostname,omitempty"`
	Vrf      string `yaml:"vrf,omitempty"`
}

type LocationsFile struct {
	Locations []LocationEntry `yaml:"locations"`
}

// NewLocationMatcher creates a new location matcher and loads from file
func NewLocationMatcher(path string, logger *zap.Logger) (*LocationMatcher, error) {
	m := &LocationMatcher{
		networks: make([]netWithLocation, 0),
		logger:   logger.Named("location_matcher"),
	}

	if err := m.Load(path); err != nil {
		return nil, err
	}

	return m, nil
}

// NewEmptyLocationMatcher creates an empty matcher
func NewEmptyLocationMatcher(logger *zap.Logger) *LocationMatcher {
	return &LocationMatcher{
		networks: make([]netWithLocation, 0),
		logger:   logger.Named("location_matcher"),
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
			vrf:      entry.Vrf,
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

	// Log loaded networks
	m.logger.Info("Locations loaded",
		zap.Int("count", len(networks)),
		zap.String("path", path))

	// Log each network at debug level
	for i, nwl := range networks {
		m.logger.Debug("Loaded network",
			zap.Int("index", i),
			zap.String("network", nwl.network.String()),
			zap.String("location", nwl.location),
			zap.String("vrf", nwl.vrf))
	}

	return nil
}

// Reload reloads locations from file
func (m *LocationMatcher) Reload(path string) error {
	return m.Load(path)
}

// GetLocation returns the best-match location for an IP (longest prefix match)
func (m *LocationMatcher) GetLocation(ip string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return "unknown"
	}

	best := ""
	bestLen := -1
	for _, nwl := range m.networks {
		if nwl.network.Contains(parsedIP) {
			ones, _ := nwl.network.Mask.Size()
			if ones > bestLen {
				bestLen = ones
				best = nwl.location
			}
		}
	}
	if bestLen < 0 {
		return "unknown"
	}

	// Debug logging for each lookup (only in debug mode)
	if m.logger != nil && m.logger.Core().Enabled(zap.DebugLevel) {
		m.logger.Debug("Looking up IP",
			zap.String("ip", ip),
			zap.Int("networks_count", len(m.networks)))
	}

	return best
}

// GetHostname returns the hostname for an IP if available (longest prefix match)
func (m *LocationMatcher) GetHostname(ip string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return ""
	}

	best := ""
	bestLen := -1
	for _, nwl := range m.networks {
		if nwl.network.Contains(parsedIP) {
			ones, _ := nwl.network.Mask.Size()
			if ones > bestLen {
				bestLen = ones
				if nwl.hostname != "" {
					best = nwl.hostname
				} else {
					best = nwl.location
				}
			}
		}
	}
	return best
}

// GetVrf returns the VRF for an IP (longest prefix match), or "unknown" if not found
func (m *LocationMatcher) GetVrf(ip string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return "unknown"
	}

	best := ""
	bestLen := -1
	for _, nwl := range m.networks {
		if nwl.network.Contains(parsedIP) {
			ones, _ := nwl.network.Mask.Size()
			if ones > bestLen {
				bestLen = ones
				if nwl.vrf != "" {
					best = nwl.vrf
				}
			}
		}
	}
	if bestLen < 0 {
		return "unknown"
	}
	if best == "" {
		return "unknown"
	}
	return best
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
