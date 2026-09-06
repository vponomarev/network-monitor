package discovery

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/vponomarev/network-monitor/internal/topology"
)

// DiscoveryService coordinates path discovery
type DiscoveryService struct {
	mu          sync.RWMutex
	tracerouter Tracerouter
	cache       *PathCache
	lossTracker *LossTracker
	topN        int
	mode        string // both, top_loss, on_demand, periodic
	interval    time.Duration
	stopCh      chan struct{}
	stopOnce    sync.Once
	metrics     *Metrics
	topology    *topology.Topology
}

// DiscoveryRequest represents an API discovery request
type DiscoveryRequest struct {
	SrcIP string `json:"src_ip"`
	DstIP string `json:"dst_ip"`
}

// DiscoveryResponse represents an API discovery response
type DiscoveryResponse struct {
	PathID               string             `json:"path_id"`
	SrcIP                string             `json:"src_ip"`
	DstIP                string             `json:"dst_ip"`
	Hops                 []Hop              `json:"hops"`
	Bottleneck           *Bottleneck        `json:"bottleneck,omitempty"`
	Discovered           time.Time          `json:"discovered"`
	DestinationProbeLoss *float64           `json:"destination_probe_loss_percent"`
	TotalLoss            float64            `json:"-"`
	AvgRTT               string             `json:"avg_rtt"`
	Topology             *topology.PathInfo `json:"topology,omitempty"`
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

// Discover performs path discovery for a specific pair
func (s *DiscoveryService) Discover(ctx context.Context, srcIP, dstIP string) (*DiscoveryResponse, error) {
	return s.discover(ctx, srcIP, dstIP, false)
}

func (s *DiscoveryService) discover(ctx context.Context, srcIP, dstIP string, fresh bool) (*DiscoveryResponse, error) {
	// Try cache first
	if path, ok := s.cache.Get(srcIP, dstIP); ok && !fresh {
		return s.pathToResponse(path), nil
	}

	// Run traceroute
	path, err := s.tracerouter.Run(ctx, srcIP, dstIP)
	if err != nil {
		return nil, err
	}

	// Cache the result
	s.cache.Set(path)
	response := s.pathToResponse(path)
	s.metrics.Observe(path, response.Bottleneck)
	return response, nil
}

// DiscoverTop performs discovery for top N lossy pairs
func (s *DiscoveryService) DiscoverTop(ctx context.Context) ([]*DiscoveryResponse, error) {
	topPairs := s.lossTracker.GetTopPairs(s.topN)
	responses := make([]*DiscoveryResponse, 0, len(topPairs))

	for _, pair := range topPairs {
		resp, err := s.discover(ctx, pair.SrcIP, pair.DstIP, true)
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
	s.stopOnce.Do(func() { close(s.stopCh) })
}

func (s *DiscoveryService) SetMetrics(metrics *Metrics) {
	s.metrics = metrics
}

func (s *DiscoveryService) SetTopology(networkTopology *topology.Topology) {
	s.mu.Lock()
	s.topology = networkTopology
	s.mu.Unlock()
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
	response := &DiscoveryResponse{
		PathID:               path.PathID(),
		SrcIP:                path.SrcIP.String(),
		DstIP:                path.DstIP.String(),
		Hops:                 path.Hops,
		Bottleneck:           bottleneck,
		Discovered:           path.Discovered,
		TotalLoss:            path.TotalLoss(),
		DestinationProbeLoss: path.DestinationProbeLoss(),
		AvgRTT:               path.AvgRTT().String(),
	}
	s.mu.RLock()
	networkTopology := s.topology
	s.mu.RUnlock()
	if networkTopology != nil {
		response.Topology = networkTopology.EnrichPath(response.SrcIP, response.DstIP)
	}
	return response
}

// HTTPHandler returns an HTTP handler for the discovery API
func (s *DiscoveryService) HTTPHandler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/discover", s.handleDiscover)
	mux.HandleFunc("/api/v1/discover/top", s.handleDiscoverTop)
	mux.HandleFunc("/api/v1/loss/top", s.handleLossTop)

	return mux
}

// RegisterHTTPHandlers mounts all discovery endpoints on the application mux.
// The trailing-slash pattern is required for nested routes such as
// /api/v1/discover/top.
func RegisterHTTPHandlers(mux *http.ServeMux, handler http.Handler) {
	mux.Handle("/api/v1/discover", handler)
	mux.Handle("/api/v1/discover/", handler)
	mux.Handle("/api/v1/loss/top", handler)
}

func (s *DiscoveryService) handleDiscover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	var req DiscoveryRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if net.ParseIP(req.SrcIP) == nil || net.ParseIP(req.DstIP) == nil {
		http.Error(w, "valid src_ip and dst_ip are required", http.StatusBadRequest)
		return
	}

	resp, err := s.Discover(r.Context(), req.SrcIP, req.DstIP)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSONResponse(w, resp)
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

	writeJSONResponse(w, responses)
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
	if limit < 1 {
		limit = 1
	}
	if limit > 100 {
		limit = 100
	}

	pairs := s.lossTracker.GetTopPairs(limit)

	writeJSONResponse(w, pairs)
}

func writeJSONResponse(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		http.Error(w, "encoding response", http.StatusInternalServerError)
	}
}
