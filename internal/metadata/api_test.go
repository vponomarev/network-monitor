package metadata

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

type fixedCounter int

func (c fixedCounter) Count() int { return int(c) }

func TestMetadataStatusIncludesConfiguredFileWithoutPoller(t *testing.T) {
	api := NewMetadataStatusAPI()
	api.RegisterSource("locations", SourceConfig{FilePath: "/etc/netmon/old-locations.yaml"})
	api.SetFilePath("locations", "/etc/netmon/locations.yaml")
	api.RegisterCounter("locations", fixedCounter(3))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/metadata/status", nil)
	response := httptest.NewRecorder()
	api.HTTPHandler().ServeHTTP(response, req)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}

	rawBody := response.Body.Bytes()
	var body StatusResponse
	if err := json.Unmarshal(rawBody, &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	status := body.Sources["locations"]
	if status.FilePath != "/etc/netmon/locations.yaml" {
		t.Fatalf("file_path = %q", status.FilePath)
	}
	if status.HTTPURL != "" {
		t.Fatalf("http_url = %q, want empty", status.HTTPURL)
	}
	if status.Enabled {
		t.Fatal("enabled = true, want false")
	}
	if status.EntriesCount != 3 {
		t.Fatalf("entries_count = %d, want 3", status.EntriesCount)
	}
	if strings.Contains(string(rawBody), "last_check") ||
		strings.Contains(string(rawBody), "last_update") {
		t.Fatalf("zero timestamps must be omitted: %s", rawBody)
	}
}

func TestMetadataStatusIncludesConfiguredUpdateURL(t *testing.T) {
	api := NewMetadataStatusAPI()
	api.RegisterSource("roles", SourceConfig{
		FilePath: "/etc/netmon/roles.yaml",
		HTTPURL:  "https://metadata.example/roles.yaml",
		Enabled:  true,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/metadata/status", nil)
	response := httptest.NewRecorder()
	api.HTTPHandler().ServeHTTP(response, req)

	var body StatusResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	status := body.Sources["roles"]
	if status.HTTPURL != "https://metadata.example/roles.yaml" {
		t.Fatalf("http_url = %q", status.HTTPURL)
	}
	if !status.Enabled {
		t.Fatal("enabled = false, want true")
	}
}

func TestMetadataStatusRejectsNonGET(t *testing.T) {
	api := NewMetadataStatusAPI()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/metadata/status", nil)
	response := httptest.NewRecorder()

	api.HTTPHandler().ServeHTTP(response, req)

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
}

func TestMetadataRefreshForceAndUnchanged(t *testing.T) {
	remote := []byte("roles:\n  - network: 10.0.0.1/32\n    role: api\n")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(remote)
	}))
	defer server.Close()

	filePath := filepath.Join(t.TempDir(), "roles.yaml")
	poller := NewHTTPPoller(HTTPPollerConfig{
		Name: "roles", URL: server.URL, Timeout: time.Second, FilePath: filePath,
	}, zap.NewNop(), prometheus.NewRegistry())
	poller.SetValidator(RolesValidator)
	reloads := 0
	poller.SetReloadFunc(func() error { reloads++; return nil })
	api := NewMetadataStatusAPI()
	api.RegisterSource("roles", SourceConfig{FilePath: filePath, HTTPURL: server.URL, Enabled: true})
	api.RegisterPoller("roles", poller)

	call := func(body string) (int, RefreshResponse) {
		t.Helper()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/metadata/refresh", strings.NewReader(body))
		response := httptest.NewRecorder()
		api.HTTPHandler().ServeHTTP(response, request)
		var decoded RefreshResponse
		if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
			t.Fatalf("decode response: %v; body=%s", err, response.Body.String())
		}
		return response.Code, decoded
	}

	code, first := call(`{"sources":["roles"],"force":false}`)
	if code != http.StatusOK || first.Sources["roles"].Status != RefreshStatusUpdated {
		t.Fatalf("first refresh: code=%d result=%+v", code, first)
	}
	code, second := call(`{"sources":["roles"],"force":false}`)
	if code != http.StatusOK || second.Sources["roles"].Status != RefreshStatusUnchanged {
		t.Fatalf("second refresh: code=%d result=%+v", code, second)
	}
	if reloads != 1 {
		t.Fatalf("reloads=%d, want 1", reloads)
	}

	if err := os.WriteFile(filePath, []byte("local drift"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, forced := call(`{"sources":["roles"]}`)
	if code != http.StatusOK || forced.Sources["roles"].Status != RefreshStatusUpdated {
		t.Fatalf("forced refresh: code=%d result=%+v", code, forced)
	}
	content, err := os.ReadFile(filePath)
	if err != nil || string(content) != string(remote) {
		t.Fatalf("forced refresh did not restore file: err=%v content=%q", err, content)
	}
}

func TestMetadataRefreshReturnsValidationError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("roles:\n  - role: missing-network\n"))
	}))
	defer server.Close()

	filePath := filepath.Join(t.TempDir(), "roles.yaml")
	original := []byte("roles: []\n")
	if err := os.WriteFile(filePath, original, 0o644); err != nil {
		t.Fatal(err)
	}
	poller := NewHTTPPoller(HTTPPollerConfig{
		Name: "roles", URL: server.URL, Timeout: time.Second, FilePath: filePath,
	}, zap.NewNop(), prometheus.NewRegistry())
	poller.SetValidator(RolesValidator)
	api := NewMetadataStatusAPI()
	api.RegisterSource("roles", SourceConfig{FilePath: filePath, HTTPURL: server.URL, Enabled: true})
	api.RegisterPoller("roles", poller)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/metadata/refresh", strings.NewReader(`{"sources":["roles"]}`))
	response := httptest.NewRecorder()
	api.HTTPHandler().ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body RefreshResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	result := body.Sources["roles"]
	if result.Status != "error" || !strings.Contains(result.Error, "validating roles metadata") ||
		!strings.Contains(result.Error, "network or networks is required") {
		t.Fatalf("validation error not returned: %+v", result)
	}
	content, err := os.ReadFile(filePath)
	if err != nil || string(content) != string(original) {
		t.Fatalf("invalid response replaced file: err=%v content=%q", err, content)
	}

	statusRequest := httptest.NewRequest(http.MethodGet, "/api/v1/metadata/status", nil)
	statusResponse := httptest.NewRecorder()
	api.HTTPHandler().ServeHTTP(statusResponse, statusRequest)
	var status StatusResponse
	if err := json.NewDecoder(statusResponse.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	rolesStatus := status.Sources["roles"]
	if rolesStatus.LastCheck == nil || rolesStatus.LastUpdate != nil || rolesStatus.UpdateSuccess {
		t.Fatalf("failed refresh status=%+v", rolesStatus)
	}
}

func TestMetadataRefreshRequestErrors(t *testing.T) {
	api := NewMetadataStatusAPI()
	api.RegisterSource("roles", SourceConfig{FilePath: "/etc/netmon/roles.yaml"})

	tests := []struct {
		method string
		body   string
		status int
	}{
		{http.MethodGet, "", http.StatusMethodNotAllowed},
		{http.MethodPost, "{", http.StatusBadRequest},
		{http.MethodPost, `{"sources":["bogus"]}`, http.StatusBadRequest},
		{http.MethodPost, `{}`, http.StatusConflict},
		{http.MethodPost, `{"sources":["roles"]}`, http.StatusInternalServerError},
	}
	for _, test := range tests {
		request := httptest.NewRequest(test.method, "/api/v1/metadata/refresh", strings.NewReader(test.body))
		response := httptest.NewRecorder()
		api.HTTPHandler().ServeHTTP(response, request)
		if response.Code != test.status {
			t.Errorf("%s %q: status=%d want=%d body=%s", test.method, test.body, response.Code, test.status, response.Body.String())
		}
		if response.Header().Get("Content-Type") != "application/json" {
			t.Errorf("content-type=%q", response.Header().Get("Content-Type"))
		}
	}
}
