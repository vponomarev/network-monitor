package metadata

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
