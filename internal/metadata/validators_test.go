package metadata

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLocationsValidator(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		wantErr bool
		errMsg  string
	}{
		{
			name:    "empty data",
			data:    []byte{},
			wantErr: true,
			errMsg:  "empty locations data",
		},
		{
			name:    "invalid YAML",
			data:    []byte("invalid: yaml: :"),
			wantErr: true,
			errMsg:  "invalid YAML",
		},
		{
			name:    "empty locations list",
			data:    []byte("locations: []"),
			wantErr: true,
			errMsg:  "empty locations",
		},
		{
			name:    "missing network",
			data:    []byte("locations:\n  - location: dc1"),
			wantErr: true,
			errMsg:  "network or networks is required",
		},
		{
			name:    "missing location",
			data:    []byte("locations:\n  - network: 10.0.0.0/8"),
			wantErr: true,
			errMsg:  "location is required",
		},
		{
			name:    "invalid CIDR",
			data:    []byte("locations:\n  - network: invalid\n    location: dc1"),
			wantErr: true,
			errMsg:  "invalid network",
		},
		{
			name: "valid single location",
			data: []byte(`locations:
  - network: 10.0.0.0/8
    location: dc1`),
			wantErr: false,
		},
		{
			name: "valid multiple locations",
			data: []byte(`locations:
  - network: 10.0.0.0/8
    location: dc1
  - network: 192.168.0.0/16
    location: dc2
    vrf: mgmt`),
			wantErr: false,
		},
		{
			name: "valid grouped locations",
			data: []byte(`locations:
  - networks: [10.10.0.0/20, 10.20.0.0/23]
    location: dc1`),
			wantErr: false,
		},
		{
			name: "valid with hostname",
			data: []byte(`locations:
  - network: 10.0.0.1/32
    location: dc1
    hostname: server-01`),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := LocationsValidator(tt.data)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestRolesValidator(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		wantErr bool
		errMsg  string
	}{
		{
			name:    "empty data",
			data:    []byte{},
			wantErr: true,
			errMsg:  "empty roles data",
		},
		{
			name:    "invalid YAML",
			data:    []byte("invalid: yaml: :"),
			wantErr: true,
			errMsg:  "invalid YAML",
		},
		{
			name:    "empty roles list",
			data:    []byte("roles: []"),
			wantErr: true,
			errMsg:  "empty roles",
		},
		{
			name:    "missing network",
			data:    []byte("roles:\n  - role: web-server"),
			wantErr: true,
			errMsg:  "network or networks is required",
		},
		{
			name:    "missing role",
			data:    []byte("roles:\n  - network: 10.0.0.0/8"),
			wantErr: true,
			errMsg:  "role is required",
		},
		{
			name:    "invalid CIDR",
			data:    []byte("roles:\n  - network: invalid\n    role: web"),
			wantErr: true,
			errMsg:  "invalid network",
		},
		{
			name: "valid single role",
			data: []byte(`roles:
  - network: 10.0.0.0/8
    role: web-server`),
			wantErr: false,
		},
		{
			name: "valid multiple roles",
			data: []byte(`roles:
  - network: 10.0.0.0/8
    role: web-server
  - network: 192.168.0.0/16
    role: db-server`),
			wantErr: false,
		},
		{
			name: "valid grouped roles",
			data: []byte(`roles:
  - networks:
      - 10.0.0.10/32
      - 10.0.0.11/32
    role: load-balancer`),
			wantErr: false,
		},
		{
			name: "valid /32 host",
			data: []byte(`roles:
  - network: 10.0.0.1/32
    role: load-balancer`),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := RolesValidator(tt.data)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestTopologyValidator(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		wantErr bool
		errMsg  string
	}{
		{
			name:    "empty data",
			data:    []byte{},
			wantErr: true,
			errMsg:  "empty topology data",
		},
		{
			name:    "invalid YAML",
			data:    []byte("invalid: yaml: :"),
			wantErr: true,
			errMsg:  "invalid YAML",
		},
		{
			name:    "valid empty devices",
			data:    []byte(`devices: []`),
			wantErr: false,
		},
		{
			name: "valid with devices",
			data: []byte(`devices:
  - id: leaf-01
    name: leaf-switch-01
    type: leaf
    management_ip: 10.0.0.1`),
			wantErr: false,
		},
		{
			name: "valid complex topology",
			data: []byte(`devices:
  - id: ss-01
    name: super-spine-01
    type: super-spine
    datacenter: DC1
    labels:
      vendor: arista
  - id: leaf-01
    name: leaf-switch-01
    type: leaf
    parent_id: ss-01`),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := TopologyValidator(tt.data)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
