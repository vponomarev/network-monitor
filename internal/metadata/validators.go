package metadata

import (
	"fmt"
	"net"

	"gopkg.in/yaml.v3"
)

// LocationsValidator валидирует locations.yaml
func LocationsValidator(data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("empty locations data")
	}

	var file LocationsFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return fmt.Errorf("invalid YAML: %w", err)
	}

	if len(file.Locations) == 0 {
		return fmt.Errorf("empty locations: no entries found")
	}

	// Валидация каждого entry
	for i, loc := range file.Locations {
		if loc.Network == "" {
			return fmt.Errorf("location[%d]: network is required", i)
		}
		if loc.Location == "" {
			return fmt.Errorf("location[%d]: location is required", i)
		}

		// Парсинг CIDR для валидации
		_, _, err := net.ParseCIDR(loc.Network)
		if err != nil {
			return fmt.Errorf("location[%d]: invalid network %q: %w", i, loc.Network, err)
		}
	}

	return nil
}

// RolesValidator валидирует roles.yaml
func RolesValidator(data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("empty roles data")
	}

	var file RolesFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return fmt.Errorf("invalid YAML: %w", err)
	}

	if len(file.Roles) == 0 {
		return fmt.Errorf("empty roles: no entries found")
	}

	for i, role := range file.Roles {
		if role.Network == "" {
			return fmt.Errorf("role[%d]: network is required", i)
		}
		if role.Role == "" {
			return fmt.Errorf("role[%d]: role is required", i)
		}

		_, _, err := net.ParseCIDR(role.Network)
		if err != nil {
			return fmt.Errorf("role[%d]: invalid network %q: %w", i, role.Network, err)
		}
	}

	return nil
}

// TopologyValidator валидирует topology.yaml
func TopologyValidator(data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("empty topology data")
	}

	// Базовая валидация YAML
	var dummy interface{}
	if err := yaml.Unmarshal(data, &dummy); err != nil {
		return fmt.Errorf("invalid YAML: %w", err)
	}

	// Topology может быть пустой (опционально)
	// При необходимости можно добавить более строгую валидацию

	return nil
}
