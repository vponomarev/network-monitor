package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vponomarev/network-monitor/internal/metadata"
	"github.com/vponomarev/network-monitor/internal/metrics"
	"go.uber.org/zap"
)

func TestRequireAuthMiddleware(t *testing.T) {
	const testToken = "test-secret-token"

	// Helper to create a handler that always returns 200 OK
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Run("no token configured - allows all requests", func(t *testing.T) {
		middleware := requireAuthMiddleware(okHandler, "")

		// Request without auth
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rr := httptest.NewRecorder()
		middleware.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
	})

	t.Run("token configured - no auth header - returns 401", func(t *testing.T) {
		middleware := requireAuthMiddleware(okHandler, testToken)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rr := httptest.NewRecorder()
		middleware.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusUnauthorized, rr.Code)
		assert.Contains(t, rr.Body.String(), "unauthorized")
	})

	t.Run("token configured - wrong auth header - returns 401", func(t *testing.T) {
		middleware := requireAuthMiddleware(okHandler, testToken)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer wrong-token")
		rr := httptest.NewRecorder()
		middleware.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusUnauthorized, rr.Code)
	})

	t.Run("token configured - correct auth header - returns 200", func(t *testing.T) {
		middleware := requireAuthMiddleware(okHandler, testToken)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer "+testToken)
		rr := httptest.NewRecorder()
		middleware.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
	})

	t.Run("token configured - token via query param is rejected", func(t *testing.T) {
		middleware := requireAuthMiddleware(okHandler, testToken)

		req := httptest.NewRequest(http.MethodGet, "/test?token=Bearer%20"+testToken, nil)
		rr := httptest.NewRecorder()
		middleware.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusUnauthorized, rr.Code)
	})

	t.Run("token configured - empty token in header - returns 401", func(t *testing.T) {
		middleware := requireAuthMiddleware(okHandler, testToken)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "")
		rr := httptest.NewRecorder()
		middleware.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusUnauthorized, rr.Code)
	})
}

func TestMetadataPollerReloadReconcilesLossSeries(t *testing.T) {
	tests := []struct {
		name       string
		contents   string
		labelNames []string
		reload     func(string, *metadata.LocationMatcher, *metadata.RoleMatcher, *metrics.Exporter) error
		wantLabels map[string]string
	}{
		{
			name: "role",
			contents: "roles:\n" +
				"  - network: 10.0.0.2/32\n" +
				"    role: database\n",
			labelNames: []string{"src_ip", "dst_ip", "src_role", "dst_role"},
			reload:     reloadRoleMetadata,
			wantLabels: map[string]string{"src_role": "unknown", "dst_role": "database"},
		},
		{
			name: "location and vrf",
			contents: "locations:\n" +
				"  - network: 10.0.0.0/24\n" +
				"    location: dc-a\n" +
				"    vrf: production\n",
			labelNames: []string{"src_ip", "dst_ip", "src_location", "dst_location", "src_vrf", "dst_vrf"},
			reload:     reloadLocationMetadata,
			wantLabels: map[string]string{
				"src_location": "dc-a", "dst_location": "dc-a",
				"src_vrf": "production", "dst_vrf": "production",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := zap.NewNop()
			locations := metadata.NewEmptyLocationMatcher(logger)
			roles := metadata.NewEmptyRoleMatcher(logger)
			registry := prometheus.NewRegistry()
			const metricName = "test_metadata_reload_loss_total"
			exporter := metrics.NewExporterWithConfig(
				metricName, locations, roles, logger, registry,
				metrics.CardinalityConfig{
					Level:      metrics.LevelIP,
					LabelNames: tt.labelNames,
				},
			)

			exporter.RecordRetransmit("10.0.0.1", "10.0.0.2")
			exporter.RecordRetransmit("10.0.0.1", "10.0.0.2")

			path := filepath.Join(t.TempDir(), tt.name+".yaml")
			require.NoError(t, os.WriteFile(path, []byte(tt.contents), 0o600))
			require.NoError(t, tt.reload(path, locations, roles, exporter))

			labels, value := singleMetric(t, registry, metricName)
			for name, want := range tt.wantLabels {
				assert.Equal(t, want, labels[name])
			}
			assert.Equal(t, float64(2), value, "counter must be preserved under the new labels")

			exporter.RecordRetransmit("10.0.0.1", "10.0.0.2")
			_, value = singleMetric(t, registry, metricName)
			assert.Equal(t, float64(3), value)
		})
	}
}

func singleMetric(t *testing.T, registry *prometheus.Registry, name string) (map[string]string, float64) {
	t.Helper()
	families, err := registry.Gather()
	require.NoError(t, err)
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		require.Len(t, family.GetMetric(), 1, "old label series must be removed")
		metric := family.GetMetric()[0]
		labels := make(map[string]string, len(metric.GetLabel()))
		for _, label := range metric.GetLabel() {
			labels[label.GetName()] = label.GetValue()
		}
		return labels, metric.GetCounter().GetValue()
	}
	t.Fatalf("metric %s not found", name)
	return nil, 0
}

// requireAuthMiddleware is a test helper that mimics the middleware from main.go
