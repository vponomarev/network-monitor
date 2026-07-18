package reload

import (
	"encoding/json"
	"net/http"
	"sync"
)

// Func applies the reloadable portion of the application configuration.
type Func func() error

// Manager serializes reloads triggered by signals and the HTTP API.
type Manager struct {
	mu     sync.Mutex
	reload Func
}

// NewManager creates a reload manager.
func NewManager(reload Func) *Manager {
	return &Manager{reload: reload}
}

// Reload applies the current configuration from disk.
func (m *Manager) Reload() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.reload()
}

// HTTPHandler returns the configuration reload API handler.
func (m *Manager) HTTPHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/config/reload", m.handleReload)
	return mux
}

func (m *Manager) handleReload(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": "error",
			"error":  "method not allowed",
		})
		return
	}

	if err := m.Reload(); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]string{"status": "reloaded"})
}
