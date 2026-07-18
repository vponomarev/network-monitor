package metadata

import (
	"encoding/json"
	"net/http"
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
	return mux
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
			lastCheck, hash, success := poller.GetStatus()
			if !lastCheck.IsZero() {
				status.LastCheck = &lastCheck
				status.LastUpdate = &lastCheck
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
