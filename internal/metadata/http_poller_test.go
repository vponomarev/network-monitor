package metadata

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestHTTPPoller_NewHTTPPoller(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	reg := prometheus.NewRegistry()

	cfg := HTTPPollerConfig{
		Name:     "test",
		URL:      "http://example.com/test.yaml",
		Interval: 10 * time.Minute,
		Timeout:  5 * time.Second,
		FilePath: "/tmp/test.yaml",
	}

	poller := NewHTTPPoller(cfg, logger, reg)

	assert.NotNil(t, poller)
	assert.Equal(t, "test", poller.config.Name)
	assert.Equal(t, "http://example.com/test.yaml", poller.config.URL)
}

func TestHTTPPoller_SetValidator(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	reg := prometheus.NewRegistry()

	cfg := HTTPPollerConfig{
		Name:     "test",
		URL:      "http://example.com/test.yaml",
		Interval: 10 * time.Minute,
		Timeout:  5 * time.Second,
		FilePath: "/tmp/test.yaml",
	}

	poller := NewHTTPPoller(cfg, logger, reg)

	validator := func(data []byte) error {
		if len(data) == 0 {
			return fmt.Errorf("empty data")
		}
		return nil
	}

	poller.SetValidator(validator)
	assert.NotNil(t, poller.validator)
}

func TestHTTPPoller_SetReloadFunc(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	reg := prometheus.NewRegistry()

	cfg := HTTPPollerConfig{
		Name:     "test",
		URL:      "http://example.com/test.yaml",
		Interval: 10 * time.Minute,
		Timeout:  5 * time.Second,
		FilePath: "/tmp/test.yaml",
	}

	poller := NewHTTPPoller(cfg, logger, reg)

	reloadCalled := false
	poller.SetReloadFunc(func() error {
		reloadCalled = true
		return nil
	})

	assert.NotNil(t, poller.reload)
	assert.False(t, reloadCalled)
}

func TestHTTPPoller_GetStatus(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	reg := prometheus.NewRegistry()

	cfg := HTTPPollerConfig{
		Name:     "test",
		URL:      "http://example.com/test.yaml",
		Interval: 10 * time.Minute,
		Timeout:  5 * time.Second,
		FilePath: "/tmp/test.yaml",
	}

	poller := NewHTTPPoller(cfg, logger, reg)

	lastCheck, _, hash, success := poller.GetStatus()
	assert.Equal(t, time.Time{}, lastCheck)
	assert.Equal(t, "", hash)
	assert.False(t, success)
}

func TestHTTPPoller_AtomicWrite(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	reg := prometheus.NewRegistry()

	// Create temp file
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.yaml")

	cfg := HTTPPollerConfig{
		Name:     "test",
		URL:      "http://example.com/test.yaml",
		Interval: 10 * time.Minute,
		Timeout:  5 * time.Second,
		FilePath: filePath,
	}

	poller := NewHTTPPoller(cfg, logger, reg)

	data := []byte("test content")
	err := poller.atomicWrite(data)
	require.NoError(t, err)

	// Verify file exists and contains correct data
	content, err := os.ReadFile(filePath)
	require.NoError(t, err)
	assert.Equal(t, data, content)

	// Verify no temp file left
	_, err = os.Stat(filePath + ".tmp")
	assert.True(t, os.IsNotExist(err))
}

func TestHTTPPoller_Fetch(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	reg := prometheus.NewRegistry()

	// Create test server
	testData := []byte("test yaml content")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(testData)
	}))
	defer server.Close()

	cfg := HTTPPollerConfig{
		Name:     "test",
		URL:      server.URL,
		Interval: 10 * time.Minute,
		Timeout:  5 * time.Second,
		FilePath: "/tmp/test.yaml",
	}

	poller := NewHTTPPoller(cfg, logger, reg)

	ctx := context.Background()
	data, err := poller.fetch(ctx)
	require.NoError(t, err)
	assert.Equal(t, testData, data)
}

func TestHTTPPoller_Fetch_ErrorStatus(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	reg := prometheus.NewRegistry()

	// Create test server that returns 404
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	cfg := HTTPPollerConfig{
		Name:     "test",
		URL:      server.URL,
		Interval: 10 * time.Minute,
		Timeout:  5 * time.Second,
		FilePath: "/tmp/test.yaml",
	}

	poller := NewHTTPPoller(cfg, logger, reg)

	ctx := context.Background()
	data, err := poller.fetch(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 404")
	assert.Nil(t, data)
}

func TestHTTPPoller_CheckAndUpdate_NoChanges(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	reg := prometheus.NewRegistry()

	// Create temp file
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.yaml")

	// Initial data
	initialData := []byte("initial content")
	err := os.WriteFile(filePath, initialData, 0644)
	require.NoError(t, err)

	// Create test server that returns same data
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(initialData)
	}))
	defer server.Close()

	cfg := HTTPPollerConfig{
		Name:     "test",
		URL:      server.URL,
		Interval: 10 * time.Minute,
		Timeout:  5 * time.Second,
		FilePath: filePath,
	}

	poller := NewHTTPPoller(cfg, logger, reg)

	ctx := context.Background()

	// First update - should succeed
	poller.checkAndUpdate(ctx)
	time.Sleep(100 * time.Millisecond)

	_, _, hash1, success1 := poller.GetStatus()
	assert.NotEmpty(t, hash1)
	assert.True(t, success1)

	// Second update - no changes
	poller.checkAndUpdate(ctx)
	time.Sleep(100 * time.Millisecond)

	_, _, hash2, success2 := poller.GetStatus()
	assert.Equal(t, hash1, hash2)
	assert.True(t, success2)
}

func TestHTTPPoller_CheckAndUpdate_WithChanges(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	reg := prometheus.NewRegistry()

	// Create temp file
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.yaml")

	// Initial data
	initialData := []byte("initial content")
	err := os.WriteFile(filePath, initialData, 0644)
	require.NoError(t, err)

	// Track reload calls
	reloadCalled := 0

	// Create test server that returns changed data
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.WriteHeader(http.StatusOK)
			w.Write(initialData)
		} else {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("updated content"))
		}
	}))
	defer server.Close()

	cfg := HTTPPollerConfig{
		Name:     "test",
		URL:      server.URL,
		Interval: 10 * time.Minute,
		Timeout:  5 * time.Second,
		FilePath: filePath,
	}

	poller := NewHTTPPoller(cfg, logger, reg)
	poller.SetReloadFunc(func() error {
		reloadCalled++
		return nil
	})

	ctx := context.Background()

	// First update
	poller.checkAndUpdate(ctx)
	time.Sleep(100 * time.Millisecond)
	_, _, hash1, _ := poller.GetStatus()

	// Second update - with changes
	poller.checkAndUpdate(ctx)
	time.Sleep(100 * time.Millisecond)
	_, _, hash2, success2 := poller.GetStatus()

	assert.NotEqual(t, hash1, hash2)
	assert.True(t, success2)
	assert.Equal(t, 2, reloadCalled)

	// Verify file content updated
	content, err := os.ReadFile(filePath)
	require.NoError(t, err)
	assert.Equal(t, "updated content", string(content))
}

func TestHTTPPoller_CheckAndUpdate_ValidationFailed(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	reg := prometheus.NewRegistry()

	// Create temp file
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.yaml")

	// Initial valid data
	initialData := []byte("initial: valid")
	err := os.WriteFile(filePath, initialData, 0644)
	require.NoError(t, err)

	// Create test server that returns invalid data
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("invalid: data"))
	}))
	defer server.Close()

	cfg := HTTPPollerConfig{
		Name:     "test",
		URL:      server.URL,
		Interval: 10 * time.Minute,
		Timeout:  5 * time.Second,
		FilePath: filePath,
	}

	poller := NewHTTPPoller(cfg, logger, reg)

	// Validator that always fails
	poller.SetValidator(func(data []byte) error {
		return fmt.Errorf("validation failed")
	})

	reloadCalled := false
	poller.SetReloadFunc(func() error {
		reloadCalled = true
		return nil
	})

	ctx := context.Background()

	// Update should fail validation
	poller.checkAndUpdate(ctx)
	time.Sleep(100 * time.Millisecond)

	_, _, _, success := poller.GetStatus()
	assert.False(t, success)
	assert.False(t, reloadCalled)

	// File should not be updated
	content, err := os.ReadFile(filePath)
	require.NoError(t, err)
	assert.Equal(t, "initial: valid", string(content))
}

func TestHTTPPollerRefreshForceRestoresLocalFile(t *testing.T) {
	logger := zap.NewNop()
	data := []byte("roles:\n  - network: 10.0.0.1/32\n    role: api\n")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(data)
	}))
	defer server.Close()

	filePath := filepath.Join(t.TempDir(), "roles.yaml")
	poller := NewHTTPPoller(HTTPPollerConfig{
		Name: "roles", URL: server.URL, Timeout: time.Second, FilePath: filePath,
	}, logger, prometheus.NewRegistry())
	poller.SetValidator(RolesValidator)
	reloads := 0
	poller.SetReloadFunc(func() error { reloads++; return nil })

	first, err := poller.Refresh(context.Background(), false)
	require.NoError(t, err)
	assert.Equal(t, RefreshStatusUpdated, first.Status)

	second, err := poller.Refresh(context.Background(), false)
	require.NoError(t, err)
	assert.Equal(t, RefreshStatusUnchanged, second.Status)
	assert.Equal(t, 1, reloads)

	require.NoError(t, os.WriteFile(filePath, []byte("locally corrupted"), 0o644))
	forced, err := poller.Refresh(context.Background(), true)
	require.NoError(t, err)
	assert.Equal(t, RefreshStatusUpdated, forced.Status)
	assert.Equal(t, 2, reloads)
	content, err := os.ReadFile(filePath)
	require.NoError(t, err)
	assert.Equal(t, data, content)
}

func TestHTTPPollerRefreshReturnsValidationError(t *testing.T) {
	logger := zap.NewNop()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("roles:\n  - role: missing-network\n"))
	}))
	defer server.Close()

	filePath := filepath.Join(t.TempDir(), "roles.yaml")
	original := []byte("roles: []\n")
	require.NoError(t, os.WriteFile(filePath, original, 0o644))
	poller := NewHTTPPoller(HTTPPollerConfig{
		Name: "roles", URL: server.URL, Timeout: time.Second, FilePath: filePath,
	}, logger, prometheus.NewRegistry())
	poller.SetValidator(RolesValidator)

	result, err := poller.Refresh(context.Background(), true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "validating roles metadata")
	assert.Contains(t, err.Error(), "network or networks is required")
	assert.Empty(t, result.Status)
	content, readErr := os.ReadFile(filePath)
	require.NoError(t, readErr)
	assert.Equal(t, original, content, "invalid HTTP data must not replace the active file")
	lastCheck, _, _, success := poller.GetStatus()
	assert.False(t, lastCheck.IsZero())
	assert.False(t, success)
}

func TestHTTPPollerRefreshSerializesConcurrentCalls(t *testing.T) {
	logger := zap.NewNop()
	var mu sync.Mutex
	active := 0
	maxActive := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		active++
		if active > maxActive {
			maxActive = active
		}
		mu.Unlock()
		time.Sleep(50 * time.Millisecond)
		_, _ = w.Write([]byte("roles:\n  - network: 10.0.0.1/32\n    role: web\n"))
		mu.Lock()
		active--
		mu.Unlock()
	}))
	defer server.Close()

	poller := NewHTTPPoller(HTTPPollerConfig{
		Name: "roles", URL: server.URL, Timeout: time.Second,
		FilePath: filepath.Join(t.TempDir(), "roles.yaml"),
	}, logger, prometheus.NewRegistry())
	poller.SetValidator(RolesValidator)

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := poller.Refresh(context.Background(), true)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	assert.Equal(t, 1, maxActive)
}

func TestHashFloat(t *testing.T) {
	// Просто проверяем что функция не паникует
	assert.NotPanics(t, func() {
		hashFloat("")
		hashFloat("abc")
		hashFloat("a1b2c3d4e5f67890")
		hashFloat("ffffffffffffffff")
	})
}

func TestValidateYAML(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		wantErr bool
	}{
		{
			name:    "empty data",
			data:    []byte{},
			wantErr: true,
		},
		{
			name:    "valid YAML",
			data:    []byte("key: value"),
			wantErr: false,
		},
		{
			name:    "valid YAML complex",
			data:    []byte("locations:\n  - network: 10.0.0.0/8\n    location: dc1"),
			wantErr: false,
		},
		{
			name:    "invalid YAML",
			data:    []byte("invalid: yaml: :"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateYAML(tt.data)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
