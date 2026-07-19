package metadata

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestUnknownTrackerObserveAndEndpoints(t *testing.T) {
	logger := zap.NewNop()
	dir := t.TempDir()
	locationsPath := filepath.Join(dir, "locations.yaml")
	rolesPath := filepath.Join(dir, "roles.yaml")
	require.NoError(t, os.WriteFile(locationsPath, []byte(`locations:
  - network: 10.0.0.1/32
    location: dc-a
    vrf: production
`), 0o600))
	require.NoError(t, os.WriteFile(rolesPath, []byte("roles: []\n"), 0o600))
	locations, err := NewLocationMatcher(locationsPath, logger)
	require.NoError(t, err)
	roles, err := NewRoleMatcher(rolesPath, logger)
	require.NoError(t, err)

	mainRegistry := prometheus.NewRegistry()
	tracker := NewUnknownTracker(locations, roles, time.Hour, 10, mainRegistry)
	tracker.ObservePair("10.0.0.1", "10.0.0.2")

	entries := tracker.snapshot("")
	require.Len(t, entries, 2)
	assert.Equal(t, []string{UnknownRole}, entries[0].Missing)
	assert.Equal(t, []string{UnknownLocation, UnknownRole, UnknownVRF}, entries[1].Missing)
	assert.Equal(t, []string{"src"}, entries[0].Directions)
	assert.Equal(t, []string{"dst"}, entries[1].Directions)

	assertMetricValue(t, mainRegistry, "netmon_metadata_unknown_ips", map[string]string{"attribute": "role"}, 2)
	assertMetricValue(t, mainRegistry, "netmon_metadata_unknown_ips", map[string]string{"attribute": "location"}, 1)
	assertMetricValue(t, mainRegistry, "netmon_metadata_unknown_events_total", map[string]string{"attribute": "vrf"}, 1)

	diagnosticRegistry := prometheus.NewRegistry()
	diagnosticRegistry.MustRegister(tracker)
	families, err := diagnosticRegistry.Gather()
	require.NoError(t, err)
	require.Len(t, families, 1)
	assert.Equal(t, "netmon_metadata_unknown_ip", families[0].GetName())
	assert.Len(t, families[0].GetMetric(), 4)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/metadata/unknown?attribute=role&limit=10", nil)
	response := httptest.NewRecorder()
	tracker.APIHandler().ServeHTTP(response, req)
	require.Equal(t, http.StatusOK, response.Code)
	var body struct {
		Entries   []UnknownEntry `json:"entries"`
		Total     int            `json:"total"`
		Truncated bool           `json:"truncated"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	assert.Equal(t, 2, body.Total)
	assert.False(t, body.Truncated)
	assert.Len(t, body.Entries, 2)
}

func TestUnknownTrackerLimitTTLAndReconcile(t *testing.T) {
	logger := zap.NewNop()
	locations := NewEmptyLocationMatcher(logger)
	roles := NewEmptyRoleMatcher(logger)
	registry := prometheus.NewRegistry()
	tracker := NewUnknownTracker(locations, roles, time.Minute, 1, registry)

	tracker.ObservePair("10.0.0.1", "10.0.0.2")
	assert.Len(t, tracker.snapshot(""), 1)
	assertMetricValue(t, registry, "netmon_metadata_unknown_observations_dropped_total", nil, 1)

	dir := t.TempDir()
	locationsPath := filepath.Join(dir, "locations.yaml")
	rolesPath := filepath.Join(dir, "roles.yaml")
	require.NoError(t, os.WriteFile(locationsPath, []byte(`locations:
  - network: 10.0.0.1/32
    location: dc-a
    vrf: production
`), 0o600))
	require.NoError(t, os.WriteFile(rolesPath, []byte(`roles:
  - network: 10.0.0.1/32
    role: database
`), 0o600))
	require.NoError(t, locations.Reload(locationsPath))
	require.NoError(t, roles.Reload(rolesPath))
	tracker.Reconcile()
	assert.Empty(t, tracker.snapshot(""))
	assertMetricValue(t, registry, "netmon_metadata_unknown_ips", map[string]string{"attribute": "role"}, 0)

	tracker.ObservePair("10.0.0.3", "10.0.0.3")
	assert.Len(t, tracker.snapshot(""), 1)
	assert.Equal(t, 1, tracker.Cleanup(time.Now().Add(2*time.Minute)))
	assert.Empty(t, tracker.snapshot(""))
}

func TestUnknownTrackerAPIValidation(t *testing.T) {
	tracker := NewUnknownTracker(
		NewEmptyLocationMatcher(zap.NewNop()),
		NewEmptyRoleMatcher(zap.NewNop()),
		time.Hour,
		10,
		prometheus.NewRegistry(),
	)
	for _, target := range []string{
		"/api/v1/metadata/unknown?attribute=hostname",
		"/api/v1/metadata/unknown?limit=0",
		"/api/v1/metadata/unknown?limit=11",
	} {
		response := httptest.NewRecorder()
		tracker.APIHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
		assert.Equal(t, http.StatusBadRequest, response.Code)
	}
	response := httptest.NewRecorder()
	tracker.APIHandler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/metadata/unknown", nil))
	assert.Equal(t, http.StatusMethodNotAllowed, response.Code)
}

func assertMetricValue(t *testing.T, registry *prometheus.Registry, name string, labels map[string]string, want float64) {
	t.Helper()
	families, err := registry.Gather()
	require.NoError(t, err)
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.GetMetric() {
			matches := true
			for label, value := range labels {
				found := false
				for _, pair := range metric.GetLabel() {
					if pair.GetName() == label && pair.GetValue() == value {
						found = true
						break
					}
				}
				if !found {
					matches = false
					break
				}
			}
			if matches {
				value := metric.GetGauge().GetValue()
				if metric.Counter != nil {
					value = metric.GetCounter().GetValue()
				}
				assert.Equal(t, want, value)
				return
			}
		}
	}
	t.Fatalf("metric %s with labels %v not found", name, labels)
}
