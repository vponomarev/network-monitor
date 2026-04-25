package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// DiscoveryService coordinates path discovery
type DiscoveryService struct {
	mu           sync.RWMutex
	tracerouter  Tracerouter
	cache        *PathCache
	lossTracker  *LossTracker
	topN         int
	mode         string // both, top_loss, on_demand, periodic
	interval     time.Duration
	stopCh       chan struct{}
}

// DiscoveryRequest represents an API discovery request
type DiscoveryRequest struct {
	SrcIP string `json:"src_ip"`
	DstIP string `json:"dst_ip"`
}

// DiscoveryResponse represents an API discovery response
type DiscoveryResponse struct {
	PathID      string     `json:"path_id"`
	SrcIP       string     `json:"src_ip"`
	DstIP       string     `json:"dst_ip"`
	Hops        []Hop      `json:"hops"`
	Bottleneck  *Bottleneck `json:"bottleneck,omitempty"`
	Discovered  time.Time  `json:"discovered"`
	TotalLoss   float64    `json:"total_loss"`
	AvgRTT      string     `json:"avg_rtt"`
}

// NewDiscoveryService creates a new discovery service
func NewDiscoveryService(
	tracerouter Tracerouter,
	cache *PathCache,
	lossTracker *LossTracker,
	topN int,
	mode string,
	interval time.Duration,
) *DiscoveryService {
	return &DiscoveryService{
		tracerouter: tracerouter,
		cache:       cache,
		lossTracker: lossTracker,
		topN:        topN,
		mode:        mode,
		interval:    interval,
		stopCh:      make(chan struct{}),
	}
}

// NewDiscoveryServiceWithFactory creates a discovery service with traceroute factory
func NewDiscoveryServiceWithFactory(
	factory *TracerouteFactory,
	cache *PathCache,
	lossTracker *LossTracker,
	topN int,
	mode string,
	interval time.Duration,
	protocol string,
) (*DiscoveryService, error) {
	tracerouter, err := factory.Create(protocol)
	if err != nil {
		return nil, fmt.Errorf("creating tracerouter: %w", err)
	}

	return NewDiscoveryService(tracerouter, cache, lossTracker, topN, mode, interval), nil
}

// DefaultDiscoveryService creates a service with default settings
func DefaultDiscoveryService() *DiscoveryService {
	return NewDiscoveryService(
		NewDefaultTracerouter(),
		DefaultPathCache(),
		DefaultLossTracker(),
		10,
		"both",
		5*time.Minute,
	)
}

// Discover performs path discovery for a specific pair
func (s *DiscoveryService) Discover(ctx context.Context, srcIP, dstIP string) (*DiscoveryResponse, error) {
	// Try cache first
	if path, ok := s.cache.Get(srcIP, dstIP); ok {
		return s.pathToResponse(path), nil
	}

	// Run traceroute
	path, err := s.tracerouter.Run(ctx, srcIP, dstIP)
	if err != nil {
		return nil, err
	}

	// Cache the result
	s.cache.Set(path)

	return s.pathToResponse(path), nil
}

// DiscoverTop performs discovery for top N lossy pairs
func (s *DiscoveryService) DiscoverTop(ctx context.Context) ([]*DiscoveryResponse, error) {
	topPairs := s.lossTracker.GetTopPairs(s.topN)
	responses := make([]*DiscoveryResponse, 0, len(topPairs))

	for _, pair := range topPairs {
		resp, err := s.Discover(ctx, pair.SrcIP, pair.DstIP)
		if err != nil {
			continue
		}
		responses = append(responses, resp)
	}

	return responses, nil
}

// StartPeriodicDiscovery starts periodic discovery for top lossy pairs
func (s *DiscoveryService) StartPeriodicDiscovery(ctx context.Context) {
	if s.mode != "periodic" && s.mode != "both" {
		return
	}

	go func() {
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-s.stopCh:
				return
			case <-ticker.C:
				_, _ = s.DiscoverTop(ctx)
			}
		}
	}()
}

// Stop stops periodic discovery
func (s *DiscoveryService) Stop() {
	close(s.stopCh)
}

// RecordLoss records a loss event (called by collector)
func (s *DiscoveryService) RecordLoss(srcIP, dstIP string) {
	s.lossTracker.RecordLoss(srcIP, dstIP)
}

// GetCache returns the path cache
func (s *DiscoveryService) GetCache() *PathCache {
	return s.cache
}

// GetLossTracker returns the loss tracker
func (s *DiscoveryService) GetLossTracker() *LossTracker {
	return s.lossTracker
}

// pathToResponse converts a Path to DiscoveryResponse
func (s *DiscoveryService) pathToResponse(path *Path) *DiscoveryResponse {
	bottleneck := FindBottleneck(path)

	return &DiscoveryResponse{
		PathID:      path.PathID(),
		SrcIP:       path.SrcIP.String(),
		DstIP:       path.DstIP.String(),
		Hops:        path.Hops,
		Bottleneck:  bottleneck,
		Discovered:  path.Discovered,
		TotalLoss:   path.TotalLoss(),
		AvgRTT:      path.AvgRTT().String(),
	}
}

// HTTPHandler returns an HTTP handler for the discovery API
func (s *DiscoveryService) HTTPHandler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/discover", s.handleDiscover)
	mux.HandleFunc("/api/v1/discover/top", s.handleDiscoverTop)
	mux.HandleFunc("/api/v1/loss/top", s.handleLossTop)

	return mux
}

func (s *DiscoveryService) handleDiscover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req DiscoveryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if req.SrcIP == "" || req.DstIP == "" {
		http.Error(w, "src_ip and dst_ip required", http.StatusBadRequest)
		return
	}

	resp, err := s.Discover(r.Context(), req.SrcIP, req.DstIP)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *DiscoveryService) handleDiscoverTop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	responses, err := s.DiscoverTop(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(responses)
}

func (s *DiscoveryService) handleLossTop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse limit query param
	limit := s.topN
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil {
			limit = parsed
		}
	}

	pairs := s.lossTracker.GetTopPairs(limit)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(pairs)
}

// Integration helpers

// NewTestDiscoveryService creates a service for testing
func NewTestDiscoveryService() *DiscoveryService {
	return NewDiscoveryService(
		NewDefaultTracerouter(),
		NewPathCache(10*time.Minute, 100),
		NewLossTracker(5*time.Minute),
		10,
		"both",
		5*time.Minute,
	)
}

// ResponseRecorder is a simple HTTP response recorder for tests
type ResponseRecorder struct {
	Code      int
	Body      string
	HeaderMap http.Header
}

func NewResponseRecorder() *ResponseRecorder {
	return &ResponseRecorder{
		Code:      http.StatusOK,
		HeaderMap: make(http.Header),
	}
}

func (r *ResponseRecorder) Header() http.Header {
	return r.HeaderMap
}

func (r *ResponseRecorder) Write(data []byte) (int, error) {
	r.Body = string(data)
	return len(data), nil
}

func (r *ResponseRecorder) WriteHeader(code int) {
	r.Code = code
}

// ValidateResponse validates a discovery response
func ValidateResponse(resp *DiscoveryResponse, srcIP, dstIP string) error {
	if resp.SrcIP != srcIP {
		return errorf("expected src_ip %s, got %s", srcIP, resp.SrcIP)
	}
	if resp.DstIP != dstIP {
		return errorf("expected dst_ip %s, got %s", dstIP, resp.DstIP)
	}
	if resp.PathID == "" {
		return errorf("path_id is empty")
	}
	return nil
}

func errorf(format string, args ...interface{}) error {
	return &testError{message: fmt.Sprintf(format, args...)}
}

type testError struct {
	message string
}

func (e *testError) Error() string {
	return e.message
}
