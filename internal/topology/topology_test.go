package topology

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTopology(t *testing.T) {
	topology := NewTopology()
	require.NotNil(t, topology)
	assert.Equal(t, 0, topology.DeviceCount())
}

func TestTopology_AddDevice(t *testing.T) {
	topology := NewTopology()

	device := &NetworkDevice{
		ID:          "leaf-01",
		Name:        "leaf-switch-01",
		Type:        DeviceTypeLeaf,
		IPAddresses: []string{"192.168.1.1"},
		Subnets:     []string{"192.168.1.0/24"},
	}

	err := topology.AddDevice(device)
	require.NoError(t, err)
	assert.Equal(t, 1, topology.DeviceCount())

	// Verify device was added
	retrieved, ok := topology.GetDevice("leaf-01")
	require.True(t, ok)
	assert.Equal(t, "leaf-switch-01", retrieved.Name)
	assert.Equal(t, DeviceTypeLeaf, retrieved.Type)
}

func TestTopology_AddDevice_EmptyID(t *testing.T) {
	topology := NewTopology()

	device := &NetworkDevice{
		Name: "test-device",
	}

	err := topology.AddDevice(device)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ID is required")
}

func TestTopology_GetDeviceByIP(t *testing.T) {
	topology := NewTopology()

	// Add device with IP and subnet
	device := &NetworkDevice{
		ID:          "leaf-01",
		Name:        "leaf-switch-01",
		Type:        DeviceTypeLeaf,
		IPAddresses: []string{"192.168.1.1"},
		Subnets:     []string{"192.168.1.0/24"},
	}
	err := topology.AddDevice(device)
	require.NoError(t, err)

	// Test direct IP match
	found, ok := topology.GetDeviceByIP("192.168.1.1")
	require.True(t, ok)
	assert.Equal(t, "leaf-01", found.ID)

	// Test subnet match
	found, ok = topology.GetDeviceByIP("192.168.1.100")
	require.True(t, ok)
	assert.Equal(t, "leaf-01", found.ID)

	// Test no match
	_, ok = topology.GetDeviceByIP("10.0.0.1")
	assert.False(t, ok)
}

func TestTopology_GetDeviceByIP_LongestPrefixMatch(t *testing.T) {
	topology := NewTopology()

	// Add overlapping subnets
	device1 := &NetworkDevice{
		ID:      "leaf-01",
		Subnets: []string{"10.0.0.0/8"},
	}
	device2 := &NetworkDevice{
		ID:      "leaf-02",
		Subnets: []string{"10.1.0.0/16"},
	}
	device3 := &NetworkDevice{
		ID:      "leaf-03",
		Subnets: []string{"10.1.2.0/24"},
	}

	topology.AddDevice(device1)
	topology.AddDevice(device2)
	topology.AddDevice(device3)

	// Should match most specific (/24)
	found, ok := topology.GetDeviceByIP("10.1.2.50")
	require.True(t, ok)
	assert.Equal(t, "leaf-03", found.ID)

	// Should match /16
	found, ok = topology.GetDeviceByIP("10.1.3.50")
	require.True(t, ok)
	assert.Equal(t, "leaf-02", found.ID)

	// Should match /8
	found, ok = topology.GetDeviceByIP("10.2.3.50")
	require.True(t, ok)
	assert.Equal(t, "leaf-01", found.ID)
}

func TestTopology_GetDevicesByType(t *testing.T) {
	topology := NewTopology()

	// Add devices of different types
	topology.AddDevice(&NetworkDevice{ID: "leaf-01", Type: DeviceTypeLeaf})
	topology.AddDevice(&NetworkDevice{ID: "leaf-02", Type: DeviceTypeLeaf})
	topology.AddDevice(&NetworkDevice{ID: "spine-01", Type: DeviceTypeSpine})
	topology.AddDevice(&NetworkDevice{ID: "spine-02", Type: DeviceTypeSpine})
	topology.AddDevice(&NetworkDevice{ID: "server-01", Type: DeviceTypeServer})

	leafDevices := topology.GetDevicesByType(DeviceTypeLeaf)
	assert.Len(t, leafDevices, 2)

	spineDevices := topology.GetDevicesByType(DeviceTypeSpine)
	assert.Len(t, spineDevices, 2)

	serverDevices := topology.GetDevicesByType(DeviceTypeServer)
	assert.Len(t, serverDevices, 1)
}

func TestTopology_GetLeafDevices(t *testing.T) {
	topology := NewTopology()

	topology.AddDevice(&NetworkDevice{ID: "leaf-01", Type: DeviceTypeLeaf})
	topology.AddDevice(&NetworkDevice{ID: "leaf-02", Type: DeviceTypeLeaf})
	topology.AddDevice(&NetworkDevice{ID: "spine-01", Type: DeviceTypeSpine})

	leafDevices := topology.GetLeafDevices()
	assert.Len(t, leafDevices, 2)
}

func TestTopology_GetSpineDevices(t *testing.T) {
	topology := NewTopology()

	topology.AddDevice(&NetworkDevice{ID: "leaf-01", Type: DeviceTypeLeaf})
	topology.AddDevice(&NetworkDevice{ID: "spine-01", Type: DeviceTypeSpine})
	topology.AddDevice(&NetworkDevice{ID: "spine-02", Type: DeviceTypeSpine})

	spineDevices := topology.GetSpineDevices()
	assert.Len(t, spineDevices, 2)
}

func TestTopology_GetSuperSpineDevices(t *testing.T) {
	topology := NewTopology()

	topology.AddDevice(&NetworkDevice{ID: "ss-01", Type: DeviceTypeSuperSpine})
	topology.AddDevice(&NetworkDevice{ID: "ss-02", Type: DeviceTypeSuperSpine})
	topology.AddDevice(&NetworkDevice{ID: "spine-01", Type: DeviceTypeSpine})

	superSpineDevices := topology.GetSuperSpineDevices()
	assert.Len(t, superSpineDevices, 2)
}

func TestTopology_GetDevicePath(t *testing.T) {
	topology := NewTopology()

	// Create simple topology: server1 -> leaf1 -> spine1 -> leaf2 -> server2
	topology.AddDevice(&NetworkDevice{
		ID:               "leaf-01",
		Type:             DeviceTypeLeaf,
		Subnets:          []string{"192.168.1.0/24"},
		ParentID:         "spine-01",
		ConnectedDevices: []string{"spine-01"},
	})
	topology.AddDevice(&NetworkDevice{
		ID:               "leaf-02",
		Type:             DeviceTypeLeaf,
		Subnets:          []string{"192.168.2.0/24"},
		ParentID:         "spine-01",
		ConnectedDevices: []string{"spine-01"},
	})
	topology.AddDevice(&NetworkDevice{
		ID:               "spine-01",
		Type:             DeviceTypeSpine,
		ConnectedDevices: []string{"leaf-01", "leaf-02"},
	})

	// Get path between devices on different leafs
	path, ok := topology.GetDevicePath("192.168.1.10", "192.168.2.10")
	require.True(t, ok)
	assert.GreaterOrEqual(t, len(path), 2)
}

func TestTopology_EnrichPath(t *testing.T) {
	topology := NewTopology()

	topology.AddDevice(&NetworkDevice{
		ID:         "leaf-01",
		Type:       DeviceTypeLeaf,
		Subnets:    []string{"192.168.1.0/24"},
		Datacenter: "DC1",
		Rack:       "RACK-01",
	})
	topology.AddDevice(&NetworkDevice{
		ID:         "leaf-02",
		Type:       DeviceTypeLeaf,
		Subnets:    []string{"192.168.2.0/24"},
		Datacenter: "DC1",
		Rack:       "RACK-02",
	})

	pathInfo := topology.EnrichPath("192.168.1.10", "192.168.2.10")

	assert.Equal(t, "192.168.1.10", pathInfo.SourceIP)
	assert.Equal(t, "192.168.2.10", pathInfo.DestinationIP)
	assert.Equal(t, "DC1", pathInfo.SourceLocation)
	assert.Equal(t, "DC1", pathInfo.DestinationLocation)
	assert.Equal(t, "RACK-01", pathInfo.SourceRack)
	assert.Equal(t, "RACK-02", pathInfo.DestinationRack)
	assert.True(t, pathInfo.CrossesRack)
	assert.False(t, pathInfo.CrossesDatacenter)
}

func TestTopology_Clear(t *testing.T) {
	topology := NewTopology()

	topology.AddDevice(&NetworkDevice{ID: "leaf-01", Type: DeviceTypeLeaf})
	topology.AddDevice(&NetworkDevice{ID: "spine-01", Type: DeviceTypeSpine})

	assert.Equal(t, 2, topology.DeviceCount())

	topology.Clear()

	assert.Equal(t, 0, topology.DeviceCount())
}

func TestTopology_GetTopologyType(t *testing.T) {
	// Three-tier topology
	topology3Tier := NewTopology()
	topology3Tier.AddDevice(&NetworkDevice{ID: "ss-01", Type: DeviceTypeSuperSpine})
	topology3Tier.AddDevice(&NetworkDevice{ID: "spine-01", Type: DeviceTypeSpine})
	topology3Tier.AddDevice(&NetworkDevice{ID: "leaf-01", Type: DeviceTypeLeaf})
	assert.Equal(t, "three-tier", topology3Tier.GetTopologyType())

	// Spine-leaf topology
	topology2Tier := NewTopology()
	topology2Tier.AddDevice(&NetworkDevice{ID: "spine-01", Type: DeviceTypeSpine})
	topology2Tier.AddDevice(&NetworkDevice{ID: "leaf-01", Type: DeviceTypeLeaf})
	assert.Equal(t, "spine-leaf", topology2Tier.GetTopologyType())

	// Unknown topology
	topologyUnknown := NewTopology()
	topologyUnknown.AddDevice(&NetworkDevice{ID: "router-01", Type: DeviceTypeRouter})
	assert.Equal(t, "unknown", topologyUnknown.GetTopologyType())
}

func TestLoad_FromFile(t *testing.T) {
	// Create temporary topology file
	tmpfile, err := os.CreateTemp("", "topology_*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpfile.Name())

	yamlContent := `devices:
  - id: leaf-01
    name: leaf-switch-01
    type: leaf
    management_ip: 192.168.1.1
    datacenter: DC1
    rack: RACK-01
    subnets:
      - 192.168.1.0/24
    labels:
      vendor: arista
`
	_, err = tmpfile.WriteString(yamlContent)
	require.NoError(t, err)
	tmpfile.Close()

	// Load topology
	topology, err := Load(tmpfile.Name())
	require.NoError(t, err)
	require.NotNil(t, topology)

	assert.Equal(t, 1, topology.DeviceCount())

	device, ok := topology.GetDevice("leaf-01")
	require.True(t, ok)
	assert.Equal(t, "leaf-switch-01", device.Name)
	assert.Equal(t, DeviceTypeLeaf, device.Type)
	assert.Equal(t, "DC1", device.Datacenter)
	assert.Equal(t, "RACK-01", device.Rack)
	assert.Equal(t, "arista", device.Labels["vendor"])
}

func TestLoad_NonExistentFile(t *testing.T) {
	topology, err := Load("/nonexistent/topology.yaml")
	require.NoError(t, err)
	require.NotNil(t, topology)
	assert.Equal(t, 0, topology.DeviceCount())
}

func TestLoad_InvalidYAML(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "topology_invalid_*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpfile.Name())

	_, err = tmpfile.WriteString("invalid: yaml: content: [")
	require.NoError(t, err)
	tmpfile.Close()

	_, err = Load(tmpfile.Name())
	assert.Error(t, err)
}

func TestParseDeviceType(t *testing.T) {
	tests := []struct {
		input    string
		expected DeviceType
	}{
		{"leaf", DeviceTypeLeaf},
		{"Leaf", DeviceTypeLeaf},
		{"access", DeviceTypeLeaf},
		{"spine", DeviceTypeSpine},
		{"aggregation", DeviceTypeSpine},
		{"super-spine", DeviceTypeSuperSpine},
		{"core", DeviceTypeSuperSpine},
		{"router", DeviceTypeRouter},
		{"gateway", DeviceTypeRouter},
		{"firewall", DeviceTypeFirewall},
		{"fw", DeviceTypeFirewall},
		{"loadbalancer", DeviceTypeLoadBalancer},
		{"lb", DeviceTypeLoadBalancer},
		{"server", DeviceTypeServer},
		{"host", DeviceTypeServer},
		{"unknown", DeviceTypeUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseDeviceType(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTopology_Concurrent(t *testing.T) {
	topology := NewTopology()

	// Add initial device
	topology.AddDevice(&NetworkDevice{
		ID:      "leaf-01",
		Type:    DeviceTypeLeaf,
		Subnets: []string{"192.168.1.0/24"},
	})

	done := make(chan bool, 100)

	// Concurrent reads
	for i := 0; i < 50; i++ {
		go func() {
			_, _ = topology.GetDeviceByIP("192.168.1.100")
			_ = topology.DeviceCount()
			_ = topology.GetLeafDevices()
			done <- true
		}()
	}

	// Concurrent writes
	for i := 0; i < 50; i++ {
		go func(id int) {
			device := &NetworkDevice{
				ID:   string(rune('a' + id)),
				Type: DeviceTypeLeaf,
			}
			_ = topology.AddDevice(device)
			done <- true
		}(i)
	}

	for i := 0; i < 100; i++ {
		<-done
	}

	// Should not panic
	assert.GreaterOrEqual(t, topology.DeviceCount(), 1)
}
