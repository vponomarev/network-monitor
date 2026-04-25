package topology

import (
	"fmt"
	"net"
	"sync"
)

// DeviceType represents the type of network device
type DeviceType string

const (
	// Leaf switch (access layer)
	DeviceTypeLeaf DeviceType = "leaf"
	// Spine switch (aggregation layer)
	DeviceTypeSpine DeviceType = "spine"
	// SuperSpine switch (core layer)
	DeviceTypeSuperSpine DeviceType = "super-spine"
	// Router (edge or core)
	DeviceTypeRouter DeviceType = "router"
	// Firewall
	DeviceTypeFirewall DeviceType = "firewall"
	// LoadBalancer
	DeviceTypeLoadBalancer DeviceType = "loadbalancer"
	// Server (end host)
	DeviceTypeServer DeviceType = "server"
	// Unknown device type
	DeviceTypeUnknown DeviceType = "unknown"
)

// NetworkDevice represents a network device in the topology
type NetworkDevice struct {
	// Unique identifier
	ID string `json:"id"`
	// Device name/hostname
	Name string `json:"name"`
	// Device type
	Type DeviceType `json:"type"`
	// Management IP address
	ManagementIP string `json:"management_ip,omitempty"`
	// IP addresses managed by this device
	IPAddresses []string `json:"ip_addresses,omitempty"`
	// IP ranges/subnets managed by this device
	Subnets []string `json:"subnets,omitempty"`
	// Rack location
	Rack string `json:"rack,omitempty"`
	// Datacenter/Location
	Datacenter string `json:"datacenter,omitempty"`
	// Parent device ID (for hierarchy)
	ParentID string `json:"parent_id,omitempty"`
	// Connected device IDs
	ConnectedDevices []string `json:"connected_devices,omitempty"`
	// Metadata labels
	Labels map[string]string `json:"labels,omitempty"`
}

// Topology represents the network topology
type Topology struct {
	mu          sync.RWMutex
	devices     map[string]*NetworkDevice
	ipIndex     map[string]string // IP -> DeviceID
	subnetIndex map[string]string // Subnet -> DeviceID
}

// NewTopology creates a new empty topology
func NewTopology() *Topology {
	return &Topology{
		devices:     make(map[string]*NetworkDevice),
		ipIndex:     make(map[string]string),
		subnetIndex: make(map[string]string),
	}
}

// AddDevice adds a device to the topology
func (t *Topology) AddDevice(device *NetworkDevice) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if device.ID == "" {
		return fmt.Errorf("device ID is required")
	}

	// Store device
	t.devices[device.ID] = device

	// Index by IP addresses
	for _, ip := range device.IPAddresses {
		t.ipIndex[ip] = device.ID
	}

	// Index by subnets
	for _, subnet := range device.Subnets {
		t.subnetIndex[subnet] = device.ID
	}

	return nil
}

// GetDevice returns a device by ID
func (t *Topology) GetDevice(id string) (*NetworkDevice, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	device, ok := t.devices[id]
	return device, ok
}

// GetDeviceByIP finds the device for an IP address
func (t *Topology) GetDeviceByIP(ip string) (*NetworkDevice, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	// Direct IP match
	if deviceID, ok := t.ipIndex[ip]; ok {
		return t.devices[deviceID], true
	}

	// Subnet match (longest prefix match)
	ipAddr := net.ParseIP(ip)
	if ipAddr == nil {
		return nil, false
	}

	var bestMatch string
	var bestPrefixLen int

	for subnet, deviceID := range t.subnetIndex {
		_, network, err := net.ParseCIDR(subnet)
		if err != nil {
			continue
		}

		if network.Contains(ipAddr) {
			prefixLen, _ := network.Mask.Size()
			if prefixLen > bestPrefixLen {
				bestPrefixLen = prefixLen
				bestMatch = deviceID
			}
		}
	}

	if bestMatch != "" {
		return t.devices[bestMatch], true
	}

	return nil, false
}

// GetDevicePath returns the path between two devices through the topology
func (t *Topology) GetDevicePath(srcIP, dstIP string) ([]*NetworkDevice, bool) {
	srcDevice, srcOk := t.GetDeviceByIP(srcIP)
	dstDevice, dstOk := t.GetDeviceByIP(dstIP)

	if !srcOk || !dstOk {
		return nil, false
	}

	// Simple path: source -> destination
	path := []*NetworkDevice{srcDevice}

	// If same device, return
	if srcDevice.ID == dstDevice.ID {
		return path, true
	}

	// Find common ancestor (simple implementation)
	// In production, would use graph traversal
	if srcDevice.ParentID != "" && srcDevice.ParentID == dstDevice.ParentID {
		// Same parent (e.g., same spine)
		parent, ok := t.GetDevice(srcDevice.ParentID)
		if ok {
			path = append(path, parent)
		}
	} else if srcDevice.Type == DeviceTypeLeaf && dstDevice.Type == DeviceTypeLeaf {
		// Leaf to Leaf through Spine
		for _, connectedID := range srcDevice.ConnectedDevices {
			if connected, ok := t.GetDevice(connectedID); ok {
				if connected.Type == DeviceTypeSpine {
					path = append(path, connected)
					break
				}
			}
		}
	}

	path = append(path, dstDevice)
	return path, true
}

// GetAllDevices returns all devices
func (t *Topology) GetAllDevices() []*NetworkDevice {
	t.mu.RLock()
	defer t.mu.RUnlock()

	devices := make([]*NetworkDevice, 0, len(t.devices))
	for _, device := range t.devices {
		devices = append(devices, device)
	}
	return devices
}

// GetDevicesByType returns devices of a specific type
func (t *Topology) GetDevicesByType(deviceType DeviceType) []*NetworkDevice {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var devices []*NetworkDevice
	for _, device := range t.devices {
		if device.Type == deviceType {
			devices = append(devices, device)
		}
	}
	return devices
}

// GetLeafDevices returns all leaf switches
func (t *Topology) GetLeafDevices() []*NetworkDevice {
	return t.GetDevicesByType(DeviceTypeLeaf)
}

// GetSpineDevices returns all spine switches
func (t *Topology) GetSpineDevices() []*NetworkDevice {
	return t.GetDevicesByType(DeviceTypeSpine)
}

// GetSuperSpineDevices returns all super-spine switches
func (t *Topology) GetSuperSpineDevices() []*NetworkDevice {
	return t.GetDevicesByType(DeviceTypeSuperSpine)
}

// DeviceCount returns the number of devices
func (t *Topology) DeviceCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.devices)
}

// Clear removes all devices
func (t *Topology) Clear() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.devices = make(map[string]*NetworkDevice)
	t.ipIndex = make(map[string]string)
	t.subnetIndex = make(map[string]string)
}

// EnrichPath enriches a path with topology information
func (t *Topology) EnrichPath(srcIP, dstIP string) *PathInfo {
	pathInfo := &PathInfo{
		SourceIP:      srcIP,
		DestinationIP: dstIP,
	}

	srcDevice, srcOk := t.GetDeviceByIP(srcIP)
	if srcOk {
		pathInfo.SourceDevice = srcDevice
		pathInfo.SourceLocation = srcDevice.Datacenter
		pathInfo.SourceRack = srcDevice.Rack
	}

	dstDevice, dstOk := t.GetDeviceByIP(dstIP)
	if dstOk {
		pathInfo.DestinationDevice = dstDevice
		pathInfo.DestinationLocation = dstDevice.Datacenter
		pathInfo.DestinationRack = dstDevice.Rack
	}

	// Get intermediate path
	if srcOk && dstOk {
		devices, ok := t.GetDevicePath(srcIP, dstIP)
		if ok {
			pathInfo.IntermediateDevices = devices
			pathInfo.CrossesDatacenter = srcDevice.Datacenter != dstDevice.Datacenter
			pathInfo.CrossesRack = srcDevice.Rack != dstDevice.Rack
		}
	}

	return pathInfo
}

// PathInfo contains enriched path information
type PathInfo struct {
	SourceIP            string           `json:"src_ip"`
	DestinationIP       string           `json:"dst_ip"`
	SourceDevice        *NetworkDevice   `json:"src_device,omitempty"`
	DestinationDevice   *NetworkDevice   `json:"dst_device,omitempty"`
	IntermediateDevices []*NetworkDevice `json:"intermediate_devices,omitempty"`
	SourceLocation      string           `json:"src_location,omitempty"`
	DestinationLocation string           `json:"dst_location,omitempty"`
	SourceRack          string           `json:"src_rack,omitempty"`
	DestinationRack     string           `json:"dst_rack,omitempty"`
	CrossesDatacenter   bool             `json:"crosses_datacenter"`
	CrossesRack         bool             `json:"crosses_rack"`
}

// GetTopologyType determines the topology type based on device relationships
func (t *Topology) GetTopologyType() string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	hasSpine := false
	hasLeaf := false
	hasSuperSpine := false

	for _, device := range t.devices {
		switch device.Type {
		case DeviceTypeSpine:
			hasSpine = true
		case DeviceTypeLeaf:
			hasLeaf = true
		case DeviceTypeSuperSpine:
			hasSuperSpine = true
		}
	}

	if hasSuperSpine && hasSpine && hasLeaf {
		return "three-tier"
	}
	if hasSpine && hasLeaf {
		return "spine-leaf"
	}
	return "unknown"
}
