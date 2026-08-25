package metadata

import (
	"bytes"
	"fmt"

	"github.com/vponomarev/network-monitor/internal/topology"
	"gopkg.in/yaml.v3"
)

func decodeYAMLStrict(data []byte, target interface{}) error {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	return decoder.Decode(target)
}

// LocationsValidator validates locations.yaml.
func LocationsValidator(data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("empty locations data")
	}

	var file LocationsFile
	if err := decodeYAMLStrict(data, &file); err != nil {
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
	if err := decodeYAMLStrict(data, &file); err != nil {
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

	if _, err := topology.Parse(data); err != nil {
		return fmt.Errorf("invalid YAML: %w", err)
	}
	return nil
}
