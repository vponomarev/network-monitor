package reload

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestReloadEndpoint(t *testing.T) {
	var calls atomic.Int32
	manager := NewManager(func() error {
		calls.Add(1)
		return nil
	})

	request := httptest.NewRequest(http.MethodPost, "/api/v1/config/reload", nil)
	response := httptest.NewRecorder()
	manager.HTTPHandler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if calls.Load() != 1 {
		t.Fatalf("reload calls = %d, want 1", calls.Load())
	}
	if !strings.Contains(response.Body.String(), `"status":"reloaded"`) {
		t.Fatalf("unexpected body: %s", response.Body.String())
	}
}

func TestReloadEndpointReportsFailure(t *testing.T) {
	manager := NewManager(func() error { return errors.New("invalid config") })
	request := httptest.NewRequest(http.MethodPost, "/api/v1/config/reload", nil)
	response := httptest.NewRecorder()

	manager.HTTPHandler().ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	if !strings.Contains(response.Body.String(), "invalid config") {
		t.Fatalf("unexpected body: %s", response.Body.String())
	}
}

func TestReloadEndpointRejectsGET(t *testing.T) {
	manager := NewManager(func() error { return nil })
	request := httptest.NewRequest(http.MethodGet, "/api/v1/config/reload", nil)
	response := httptest.NewRecorder()

	manager.HTTPHandler().ServeHTTP(response, request)

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
	if response.Header().Get("Allow") != http.MethodPost {
		t.Fatalf("Allow = %q, want POST", response.Header().Get("Allow"))
	}
}
