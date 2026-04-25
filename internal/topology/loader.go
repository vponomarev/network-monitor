package topology

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// TopologyConfig represents the YAML configuration format
type TopologyConfig struct {
	Devices []DeviceConfig `yaml:"devices"`
}

// DeviceConfig represents a device in YAML format
type DeviceConfig struct {
	ID               string            `yaml:"id"`
	Name             string            `yaml:"name"`
	Type             string            `yaml:"type"`
	ManagementIP     string            `yaml:"management_ip,omitempty"`
	IPAddresses      []string          `yaml:"ip_addresses,omitempty"`
	Subnets          []string          `yaml:"subnets,omitempty"`
	Rack             string            `yaml:"rack,omitempty"`
	Datacenter       string            `yaml:"datacenter,omitempty"`
	ParentID         string            `yaml:"parent_id,omitempty"`
	ConnectedDevices []string          `yaml:"connected_devices,omitempty"`
	Labels           map[string]string `yaml:"labels,omitempty"`
}

// Load loads topology from a YAML file
func Load(path string) (*Topology, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return NewTopology(), nil
		}
		return nil, fmt.Errorf("reading topology file: %w", err)
	}

	var config TopologyConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parsing topology file: %w", err)
	}

	topology := NewTopology()

	for _, deviceConfig := range config.Devices {
		device := &NetworkDevice{
			ID:               deviceConfig.ID,
			Name:             deviceConfig.Name,
			Rack:             deviceConfig.Rack,
			Datacenter:       deviceConfig.Datacenter,
			ParentID:         deviceConfig.ParentID,
			ConnectedDevices: deviceConfig.ConnectedDevices,
			Labels:           deviceConfig.Labels,
			ManagementIP:     deviceConfig.ManagementIP,
			IPAddresses:      deviceConfig.IPAddresses,
			Subnets:          deviceConfig.Subnets,
		}

		// Parse device type
		device.Type = parseDeviceType(deviceConfig.Type)

		if err := topology.AddDevice(device); err != nil {
			return nil, fmt.Errorf("adding device %s: %w", deviceConfig.ID, err)
		}
	}

	return topology, nil
}

// parseDeviceType converts string to DeviceType
func parseDeviceType(s string) DeviceType {
	switch s {
	case "leaf", "Leaf", "LEAF", "access":
		return DeviceTypeLeaf
	case "spine", "Spine", "SPINE", "aggregation":
		return DeviceTypeSpine
	case "super-spine", "SuperSpine", "SUPER-SPINE", "core":
		return DeviceTypeSuperSpine
	case "router", "Router", "ROUTER", "gateway":
		return DeviceTypeRouter
	case "firewall", "Firewall", "FIREWALL", "fw":
		return DeviceTypeFirewall
	case "loadbalancer", "LoadBalancer", "LOADBALANCER", "lb":
		return DeviceTypeLoadBalancer
	case "server", "Server", "SERVER", "host":
		return DeviceTypeServer
	default:
		return DeviceTypeUnknown
	}
}

// Save saves topology to a YAML file
func (t *Topology) Save(path string) error {
	t.mu.RLock()
	defer t.mu.RUnlock()

	config := TopologyConfig{
		Devices: make([]DeviceConfig, 0, len(t.devices)),
	}

	for _, device := range t.devices {
		deviceConfig := DeviceConfig{
			ID:               device.ID,
			Name:             device.Name,
			Type:             string(device.Type),
			ManagementIP:     device.ManagementIP,
			IPAddresses:      device.IPAddresses,
			Subnets:          device.Subnets,
			Rack:             device.Rack,
			Datacenter:       device.Datacenter,
			ParentID:         device.ParentID,
			ConnectedDevices: device.ConnectedDevices,
			Labels:           device.Labels,
		}
		config.Devices = append(config.Devices, deviceConfig)
	}

	data, err := yaml.Marshal(&config)
	if err != nil {
		return fmt.Errorf("marshaling topology: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing topology file: %w", err)
	}

	return nil
}

// Reload reloads topology from file
func (t *Topology) Reload(path string) error {
	newTopology, err := Load(path)
	if err != nil {
		return err
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	// Copy data from new topology
	t.devices = newTopology.devices
	t.ipIndex = newTopology.ipIndex
	t.subnetIndex = newTopology.subnetIndex

	return nil
}
