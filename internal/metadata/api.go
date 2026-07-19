package metadata

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"sync"
	"time"
)

// MetadataStatusAPI provides HTTP API for metadata status
type MetadataStatusAPI struct {
	mu       sync.RWMutex
	pollers  map[string]*HTTPPoller
	counters map[string]CounterProvider
	sources  map[string]SourceConfig
}

// SourceConfig describes the configured local file and optional HTTP updater.
type SourceConfig struct {
	FilePath string
	HTTPURL  string
	Enabled  bool
}

// CounterProvider interface for getting counts from matchers
type CounterProvider interface {
	Count() int
}

// StatusResponse represents the API response
type StatusResponse struct {
	Sources map[string]SourceStatus `json:"sources"`
}

// RefreshRequest selects HTTP metadata sources. Force defaults to true when
// omitted, matching the endpoint's explicit refresh semantics.
type RefreshRequest struct {
	Sources []string `json:"sources,omitempty"`
	Force   *bool    `json:"force,omitempty"`
}

type RefreshSourceResult struct {
	RefreshResult
	Error string `json:"error,omitempty"`
}

type RefreshResponse struct {
	Sources map[string]RefreshSourceResult `json:"sources"`
}

// SourceStatus represents status of a single metadata source
type SourceStatus struct {
	FilePath      string     `json:"file_path"`
	HTTPURL       string     `json:"http_url,omitempty"`
	LastCheck     *time.Time `json:"last_check,omitempty"`
	LastHash      string     `json:"last_hash,omitempty"`
	LastUpdate    *time.Time `json:"last_update,omitempty"`
	UpdateSuccess bool       `json:"update_success"`
	EntriesCount  int        `json:"entries_count"`
	Enabled       bool       `json:"enabled"`
}

// NewMetadataStatusAPI creates a new metadata status API
func NewMetadataStatusAPI() *MetadataStatusAPI {
	return &MetadataStatusAPI{
		pollers:  make(map[string]*HTTPPoller),
		counters: make(map[string]CounterProvider),
		sources:  make(map[string]SourceConfig),
	}
}

// RegisterSource registers configured source details independently of a poller.
func (api *MetadataStatusAPI) RegisterSource(name string, source SourceConfig) {
	api.mu.Lock()
	defer api.mu.Unlock()
	api.sources[name] = source
}

// SetFilePath updates the active local file after a successful config reload.
func (api *MetadataStatusAPI) SetFilePath(name, filePath string) {
	api.mu.Lock()
	defer api.mu.Unlock()
	source := api.sources[name]
	source.FilePath = filePath
	api.sources[name] = source
}

// RegisterPoller registers a poller for status reporting
func (api *MetadataStatusAPI) RegisterPoller(name string, poller *HTTPPoller) {
	api.mu.Lock()
	defer api.mu.Unlock()
	api.pollers[name] = poller
}

// RegisterCounter registers a counter provider for entry count
func (api *MetadataStatusAPI) RegisterCounter(name string, counter CounterProvider) {
	api.mu.Lock()
	defer api.mu.Unlock()
	api.counters[name] = counter
}

// HTTPHandler returns HTTP handler for metadata status endpoint
func (api *MetadataStatusAPI) HTTPHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/metadata/status", api.handleStatus)
	mux.HandleFunc("/api/v1/metadata/refresh", api.handleRefresh)
	return mux
}

func (api *MetadataStatusAPI) handleRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var request RefreshRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := decoder.Decode(&request); err != nil && !errors.Is(err, io.EOF) {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeJSONError(w, http.StatusBadRequest, "request body must contain one JSON object")
		return
	}

	force := true
	if request.Force != nil {
		force = *request.Force
	}

	api.mu.RLock()
	pollers := make(map[string]*HTTPPoller, len(api.pollers))
	for name, poller := range api.pollers {
		pollers[name] = poller
	}
	configured := make(map[string]SourceConfig, len(api.sources))
	for name, source := range api.sources {
		configured[name] = source
	}
	api.mu.RUnlock()

	sources, err := refreshSources(request.Sources, pollers, configured)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(sources) == 0 {
		writeJSONError(w, http.StatusConflict, "no HTTP metadata update sources are configured")
		return
	}

	response := RefreshResponse{Sources: make(map[string]RefreshSourceResult, len(sources))}
	hasError := false
	for _, source := range sources {
		poller, ok := pollers[source]
		if !ok {
			cfg := configured[source]
			response.Sources[source] = RefreshSourceResult{
				RefreshResult: RefreshResult{
					Status:   "error",
					FilePath: cfg.FilePath,
					HTTPURL:  cfg.HTTPURL,
				},
				Error: "HTTP update source is not configured",
			}
			hasError = true
			continue
		}
		result, refreshErr := poller.Refresh(r.Context(), force)
		if refreshErr != nil {
			result.Status = "error"
			response.Sources[source] = RefreshSourceResult{
				RefreshResult: result,
				Error:         refreshErr.Error(),
			}
			hasError = true
			continue
		}
		response.Sources[source] = RefreshSourceResult{RefreshResult: result}
	}

	w.Header().Set("Content-Type", "application/json")
	if hasError {
		w.WriteHeader(http.StatusInternalServerError)
	}
	_ = json.NewEncoder(w).Encode(response)
}

func refreshSources(requested []string, pollers map[string]*HTTPPoller, configured map[string]SourceConfig) ([]string, error) {
	if len(requested) == 0 {
		sources := make([]string, 0, len(pollers))
		for source := range pollers {
			sources = append(sources, source)
		}
		sort.Strings(sources)
		return sources, nil
	}

	seen := make(map[string]bool, len(requested))
	sources := make([]string, 0, len(requested))
	for _, source := range requested {
		if _, known := configured[source]; !known {
			return nil, errors.New("unknown metadata source: " + source)
		}
		if !seen[source] {
			seen[source] = true
			sources = append(sources, source)
		}
	}
	return sources, nil
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func (api *MetadataStatusAPI) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	api.mu.RLock()
	defer api.mu.RUnlock()

	response := StatusResponse{
		Sources: make(map[string]SourceStatus),
	}

	// Build status for each source
	sources := []string{"locations", "roles", "topology"}
	for _, source := range sources {
		configured := api.sources[source]
		status := SourceStatus{
			FilePath: configured.FilePath,
			HTTPURL:  configured.HTTPURL,
			Enabled:  configured.Enabled,
		}

		// Get poller status if registered
		if poller, ok := api.pollers[source]; ok {
			status.Enabled = true
			lastCheck, lastUpdate, hash, success := poller.GetStatus()
			if !lastCheck.IsZero() {
				status.LastCheck = &lastCheck
			}
			if !lastUpdate.IsZero() {
				status.LastUpdate = &lastUpdate
			}
			status.LastHash = hash
			status.UpdateSuccess = success
			// A running poller is the authoritative active source.
			status.HTTPURL = poller.config.URL
			status.FilePath = poller.config.FilePath
		}

		// Get counter if registered
		if counter, ok := api.counters[source]; ok {
			status.EntriesCount = counter.Count()
		}

		response.Sources[source] = status
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
