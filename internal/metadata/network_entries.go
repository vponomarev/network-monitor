package metadata

import (
	"fmt"
	"net"
	"strings"
)

func parseEntryNetworks(single string, multiple []string, entryType string, index int) ([]*net.IPNet, error) {
	if single != "" && len(multiple) > 0 {
		return nil, fmt.Errorf("%s[%d]: network and networks are mutually exclusive", entryType, index)
	}

	values := multiple
	if single != "" {
		values = []string{single}
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("%s[%d]: network or networks is required", entryType, index)
	}

	parsed := make([]*net.IPNet, 0, len(values))
	for networkIndex, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, fmt.Errorf("%s[%d]: networks[%d] must not be empty", entryType, index, networkIndex)
		}
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			return nil, fmt.Errorf("%s[%d]: invalid network %q: %w", entryType, index, value, err)
		}
		parsed = append(parsed, network)
	}
	return parsed, nil
}

func parseRoleEntries(entries []RoleEntry) ([]netWithRole, error) {
	networks := make([]netWithRole, 0, len(entries))
	seen := make(map[string]string)

	for index, entry := range entries {
		if entry.Role == "" {
			return nil, fmt.Errorf("role[%d]: role is required", index)
		}
		parsed, err := parseEntryNetworks(entry.Network, entry.Networks, "role", index)
		if err != nil {
			return nil, err
		}
		for _, network := range parsed {
			cidr := network.String()
			if existingRole, ok := seen[cidr]; ok {
				if existingRole != entry.Role {
					return nil, fmt.Errorf("role[%d]: network %q conflicts with role %q", index, cidr, existingRole)
				}
				continue
			}
			seen[cidr] = entry.Role
			networks = append(networks, netWithRole{network: network, role: entry.Role})
		}
	}
	return networks, nil
}

type locationAttributes struct {
	location string
	hostname string
	vrf      string
}

func parseLocationEntries(entries []LocationEntry) ([]netWithLocation, error) {
	networks := make([]netWithLocation, 0, len(entries))
	seen := make(map[string]locationAttributes)

	for index, entry := range entries {
		if entry.Location == "" {
			return nil, fmt.Errorf("location[%d]: location is required", index)
		}
		parsed, err := parseEntryNetworks(entry.Network, entry.Networks, "location", index)
		if err != nil {
			return nil, err
		}
		attributes := locationAttributes{
			location: entry.Location,
			hostname: entry.Hostname,
			vrf:      entry.Vrf,
		}
		for _, network := range parsed {
			cidr := network.String()
			if existing, ok := seen[cidr]; ok {
				if existing != attributes {
					return nil, fmt.Errorf("location[%d]: network %q has conflicting location attributes", index, cidr)
				}
				continue
			}
			seen[cidr] = attributes
			networks = append(networks, netWithLocation{
				network:  network,
				location: entry.Location,
				hostname: entry.Hostname,
				vrf:      entry.Vrf,
			})
		}
	}
	return networks, nil
}
