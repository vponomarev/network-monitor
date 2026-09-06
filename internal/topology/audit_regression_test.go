package topology

import "testing"

func TestAuditDefaultRoute(t *testing.T) {
	topo := NewTopology()
	if err := topo.AddDevice(&NetworkDevice{ID: "default", Subnets: []string{"0.0.0.0/0"}}); err != nil {
		t.Fatal(err)
	}
	if _, ok := topo.GetDeviceByIP("192.0.2.1"); !ok {
		t.Fatal("valid /0 subnet never matches")
	}
}

func TestAuditDisconnectedPath(t *testing.T) {
	topo := NewTopology()
	for _, d := range []*NetworkDevice{{ID: "a", IPAddresses: []string{"192.0.2.1"}}, {ID: "b", IPAddresses: []string{"192.0.2.2"}}} {
		if err := topo.AddDevice(d); err != nil {
			t.Fatal(err)
		}
	}
	path, ok := topo.GetDevicePath("192.0.2.1", "192.0.2.2")
	if ok {
		t.Fatalf("unconnected devices reported as a path with %d devices", len(path))
	}
}
