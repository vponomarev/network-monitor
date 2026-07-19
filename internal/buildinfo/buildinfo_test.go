package buildinfo

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInfoHTTPAndMetric(t *testing.T) {
	info := New("netmon", "v2.5.1", "abc1234", "2026-07-19T00:00:00Z")

	recorder := httptest.NewRecorder()
	info.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/version", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
	var response Info
	require.NoError(t, json.NewDecoder(recorder.Body).Decode(&response))
	assert.Equal(t, info, response)

	registry := prometheus.NewRegistry()
	require.NoError(t, registry.Register(info.Collector()))
	families, err := registry.Gather()
	require.NoError(t, err)
	require.Len(t, families, 1)
	assert.Equal(t, "netmon_build_info", families[0].GetName())
	require.Len(t, families[0].GetMetric(), 1)
	metric := families[0].GetMetric()[0]
	assert.Equal(t, float64(1), metric.GetGauge().GetValue())
	labels := make(map[string]string, len(metric.GetLabel()))
	for _, label := range metric.GetLabel() {
		labels[label.GetName()] = label.GetValue()
	}
	assert.Equal(t, "v2.5.1", labels["version"])
	assert.Equal(t, "abc1234", labels["git_commit"])
	assert.Equal(t, "2026-07-19T00:00:00Z", labels["build_time"])
}

func TestInfoVersionTextAndMethodValidation(t *testing.T) {
	info := New("conntrack", "v2.5.1", "abc1234", "2026-07-19T00:00:00Z")
	var output bytes.Buffer
	require.NoError(t, info.WriteText(&output))
	assert.Contains(t, output.String(), "conntrack v2.5.1")
	assert.Contains(t, output.String(), "commit=abc1234")

	recorder := httptest.NewRecorder()
	info.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/version", nil))
	assert.Equal(t, http.StatusMethodNotAllowed, recorder.Code)
	assert.Equal(t, http.MethodGet, recorder.Header().Get("Allow"))
}
