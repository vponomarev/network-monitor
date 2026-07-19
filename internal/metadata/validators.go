package metadata

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// LocationsValidator validates locations.yaml.
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
	if _, err := parseLocationEntries(file.Locations); err != nil {
		return err
	}
	return nil
}

// RolesValidator validates roles.yaml.
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
	if _, err := parseRoleEntries(file.Roles); err != nil {
		return err
	}
	return nil
}

// TopologyValidator validates topology.yaml syntax.
func TopologyValidator(data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("empty topology data")
	}

	var document interface{}
	if err := yaml.Unmarshal(data, &document); err != nil {
		return fmt.Errorf("invalid YAML: %w", err)
	}
	return nil
}
