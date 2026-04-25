package discovery

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDiscoveryService(t *testing.T) {
	tracerouter := NewDefaultTracerouter()
	cache := DefaultPathCache()
	lossTracker := DefaultLossTracker()

	service := NewDiscoveryService(tracerouter, cache, lossTracker, 10, "both", 5*time.Minute)
	require.NotNil(t, service)
	assert.Equal(t, 10, service.topN)
	assert.Equal(t, "both", service.mode)
}

func TestDefaultDiscoveryService(t *testing.T) {
	service := DefaultDiscoveryService()
	require.NotNil(t, service)
}

func TestDiscoveryService_Discover(t *testing.T) {
	service := NewTestDiscoveryService()
	ctx := context.Background()

	resp, err := service.Discover(ctx, "192.168.1.1", "8.8.8.8")
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "192.168.1.1", resp.SrcIP)
	assert.Equal(t, "8.8.8.8", resp.DstIP)
	assert.NotEmpty(t, resp.PathID)
}

func TestDiscoveryService_Discover_Cached(t *testing.T) {
	service := NewTestDiscoveryService()
	ctx := context.Background()

	// First call
	resp1, err := service.Discover(ctx, "192.168.1.1", "8.8.8.8")
	require.NoError(t, err)

	// Second call (should be cached)
	resp2, err := service.Discover(ctx, "192.168.1.1", "8.8.8.8")
	require.NoError(t, err)

	assert.Equal(t, resp1.PathID, resp2.PathID)
}

func TestDiscoveryService_RecordLoss(t *testing.T) {
	service := NewTestDiscoveryService()

	service.RecordLoss("192.168.1.1", "192.168.1.2")
	service.RecordLoss("192.168.1.1", "192.168.1.2")

	assert.Equal(t, 1, service.lossTracker.Count())

	pair, ok := service.lossTracker.GetPair("192.168.1.1", "192.168.1.2")
	require.True(t, ok)
	assert.Equal(t, uint64(2), pair.LossCount)
}

func TestDiscoveryService_DiscoverTop(t *testing.T) {
	service := NewTestDiscoveryService()
	ctx := context.Background()

	// Record some losses
	for i := 0; i < 5; i++ {
		service.RecordLoss("192.168.1.1", "192.168.1.2")
	}
	for i := 0; i < 3; i++ {
		service.RecordLoss("192.168.1.3", "192.168.1.4")
	}

	responses, err := service.DiscoverTop(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, responses)
}

func TestDiscoveryService_pathToResponse(t *testing.T) {
	service := NewTestDiscoveryService()

	path := &Path{
		SrcIP: mustParseIP("192.168.1.1"),
		DstIP: mustParseIP("192.168.1.2"),
		Hops: []Hop{
			{TTL: 1, IP: mustParseIP("10.0.0.1"), Lost: false},
			{TTL: 2, IP: mustParseIP("10.0.0.2"), Lost: true},
		},
		Discovered: time.Now(),
	}

	resp := service.pathToResponse(path)

	assert.Equal(t, "path-192.168.1.1-192.168.1.2", resp.PathID)
	assert.Equal(t, 2, len(resp.Hops))
	assert.NotNil(t, resp.Bottleneck)
}

func TestDiscoveryService_HTTPHandler_Discover(t *testing.T) {
	service := NewTestDiscoveryService()
	handler := service.HTTPHandler()

	// Create request
	reqBody := `{"src_ip": "192.168.1.1", "dst_ip": "8.8.8.8"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/discover", bytes.NewBufferString(reqBody))
	rec := NewResponseRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp DiscoveryResponse
	err := json.Unmarshal([]byte(rec.Body), &resp)
	require.NoError(t, err)
	assert.Equal(t, "192.168.1.1", resp.SrcIP)
	assert.Equal(t, "8.8.8.8", resp.DstIP)
}

func TestDiscoveryService_HTTPHandler_Discover_InvalidMethod(t *testing.T) {
	service := NewTestDiscoveryService()
	handler := service.HTTPHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/discover", nil)
	rec := NewResponseRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestDiscoveryService_HTTPHandler_Discover_MissingFields(t *testing.T) {
	service := NewTestDiscoveryService()
	handler := service.HTTPHandler()

	// Missing dst_ip
	reqBody := `{"src_ip": "192.168.1.1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/discover", bytes.NewBufferString(reqBody))
	rec := NewResponseRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestDiscoveryService_HTTPHandler_DiscoverTop(t *testing.T) {
	service := NewTestDiscoveryService()
	handler := service.HTTPHandler()

	// Record some losses first
	service.RecordLoss("192.168.1.1", "192.168.1.2")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/discover/top", nil)
	rec := NewResponseRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var responses []*DiscoveryResponse
	err := json.Unmarshal([]byte(rec.Body), &responses)
	require.NoError(t, err)
	assert.NotEmpty(t, responses)
}

func TestDiscoveryService_HTTPHandler_LossTop(t *testing.T) {
	service := NewTestDiscoveryService()
	handler := service.HTTPHandler()

	// Record some losses
	for i := 0; i < 10; i++ {
		service.RecordLoss("192.168.1.1", "192.168.1.2")
	}
	for i := 0; i < 5; i++ {
		service.RecordLoss("192.168.1.3", "192.168.1.4")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/loss/top?limit=5", nil)
	rec := NewResponseRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var pairs []*LossPair
	err := json.Unmarshal([]byte(rec.Body), &pairs)
	require.NoError(t, err)
	assert.Len(t, pairs, 2) // We only have 2 unique pairs
}

func TestDiscoveryService_StartPeriodicDiscovery(t *testing.T) {
	service := NewTestDiscoveryService()
	ctx, cancel := context.WithCancel(context.Background())

	// Record a loss
	service.RecordLoss("192.168.1.1", "192.168.1.2")

	// Start periodic discovery
	service.interval = 10 * time.Millisecond
	service.StartPeriodicDiscovery(ctx)

	// Wait for at least one run
	time.Sleep(20 * time.Millisecond)

	// Should have discovered paths
	assert.Greater(t, service.cache.Count(), 0)

	cancel()
	service.Stop()
}

func TestDiscoveryService_GetCache(t *testing.T) {
	service := NewTestDiscoveryService()
	cache := service.GetCache()
	require.NotNil(t, cache)
}

func TestDiscoveryService_GetLossTracker(t *testing.T) {
	service := NewTestDiscoveryService()
	tracker := service.GetLossTracker()
	require.NotNil(t, tracker)
}

func TestValidateResponse(t *testing.T) {
	resp := &DiscoveryResponse{
		PathID: "path-001",
		SrcIP:  "192.168.1.1",
		DstIP:  "192.168.1.2",
	}

	err := ValidateResponse(resp, "192.168.1.1", "192.168.1.2")
	assert.NoError(t, err)
}

func TestValidateResponse_InvalidSrcIP(t *testing.T) {
	resp := &DiscoveryResponse{
		PathID: "path-001",
		SrcIP:  "192.168.1.1",
		DstIP:  "192.168.1.2",
	}

	err := ValidateResponse(resp, "192.168.1.3", "192.168.1.2")
	assert.Error(t, err)
}

func TestValidateResponse_EmptyPathID(t *testing.T) {
	resp := &DiscoveryResponse{
		PathID: "",
		SrcIP:  "192.168.1.1",
		DstIP:  "192.168.1.2",
	}

	err := ValidateResponse(resp, "192.168.1.1", "192.168.1.2")
	assert.Error(t, err)
}

func TestResponseRecorder(t *testing.T) {
	rec := NewResponseRecorder()

	// Test Header
	h := rec.Header()
	h.Set("Content-Type", "application/json")
	assert.Equal(t, "application/json", rec.HeaderMap.Get("Content-Type"))

	// Test Write
	n, err := rec.Write([]byte("test body"))
	require.NoError(t, err)
	assert.Equal(t, 9, n)
	assert.Equal(t, "test body", rec.Body)

	// Test WriteHeader
	rec.WriteHeader(http.StatusCreated)
	assert.Equal(t, http.StatusCreated, rec.Code)
}
