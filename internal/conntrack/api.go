package conntrack

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"time"
)

// API provides HTTP handlers for connection tracking
type API struct {
	tracker *Tracker
}

// NewAPI creates a new connection tracking API
func NewAPI(tracker *Tracker) *API {
	return &API{
		tracker: tracker,
	}
}

// ConnectionResponse represents a connection in API response
type ConnectionResponse struct {
	ID            string    `json:"id"`
	SourceIP      string    `json:"src_ip"`
	SourcePort    uint16    `json:"src_port"`
	DestIP        string    `json:"dst_ip"`
	DestPort      uint16    `json:"dst_port"`
	Protocol      string    `json:"protocol"`
	Direction     string    `json:"direction"`
	State         string    `json:"state"`
	PID           uint32    `json:"pid,omitempty"`
	ProcessName   string    `json:"process_name,omitempty"`
	Timestamp     time.Time `json:"timestamp"`
	LastUpdated   time.Time `json:"last_updated"`
	Established   bool      `json:"established,omitempty"`
	BytesSent     uint64    `json:"bytes_sent,omitempty"`
	BytesRecv     uint64    `json:"bytes_recv,omitempty"`
	Duration      string    `json:"duration,omitempty"`
	HandshakeTime string    `json:"handshake_time,omitempty"`
}

// StatsResponse represents connection statistics
type StatsResponse struct {
	TotalOutgoing    int `json:"total_outgoing"`
	TotalIncoming    int `json:"total_incoming"`
	PendingOutgoing  int `json:"pending_outgoing"`
	PendingIncoming  int `json:"pending_incoming"`
	Established      int `json:"established"`
	Total            int `json:"total"`
}

// ListConnections handles GET /api/v1/conntrack/connections
func (a *API) ListConnections(w http.ResponseWriter, r *http.Request) {
	if a.tracker == nil {
		http.Error(w, "Connection tracker not available", http.StatusServiceUnavailable)
		return
	}

	// Parse query parameters
	limitStr := r.URL.Query().Get("limit")
	limit := 100 // default limit
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	stateFilter := r.URL.Query().Get("state")
	directionFilter := r.URL.Query().Get("direction")

	// Get connections
	conns := a.tracker.GetConnections()

	// Filter and convert connections
	var response []ConnectionResponse
	for _, conn := range conns {
		// Apply filters
		if stateFilter != "" && conn.State.String() != stateFilter {
			continue
		}
		if directionFilter != "" && conn.Direction.String() != directionFilter {
			continue
		}

		resp := ConnectionResponse{
			ID:          conn.ID,
			SourceIP:    conn.SourceIP.String(),
			SourcePort:  conn.SourcePort,
			DestIP:      conn.DestIP.String(),
			DestPort:    conn.DestPort,
			Protocol:    protocolToString(conn.Protocol),
			Direction:   conn.Direction.String(),
			State:       conn.State.String(),
			PID:         conn.PID,
			ProcessName: conn.ProcessName,
			Timestamp:   conn.Timestamp,
			LastUpdated: conn.LastUpdated,
			Established: conn.Established,
			BytesSent:   conn.BytesSent,
			BytesRecv:   conn.BytesRecv,
			Duration:    conn.Duration().String(),
		}

		if hs := conn.HandshakeDuration(); hs > 0 {
			resp.HandshakeTime = hs.String()
		}

		response = append(response, resp)
	}

	// Sort by timestamp (newest first)
	sort.Slice(response, func(i, j int) bool {
		return response[i].Timestamp.After(response[j].Timestamp)
	})

	// Apply limit
	if len(response) > limit {
		response = response[:limit]
	}

	// Return response
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// GetStats handles GET /api/v1/conntrack/stats
func (a *API) GetStats(w http.ResponseWriter, r *http.Request) {
	if a.tracker == nil {
		http.Error(w, "Connection tracker not available", http.StatusServiceUnavailable)
		return
	}

	stats := a.tracker.GetStats()
	response := StatsResponse{
		TotalOutgoing:   stats.TotalOutgoing,
		TotalIncoming:   stats.TotalIncoming,
		PendingOutgoing: stats.PendingOutgoing,
		PendingIncoming: stats.PendingIncoming,
		Established:     stats.Established,
		Total:           stats.TotalOutgoing + stats.TotalIncoming,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// HTTPHandler returns an HTTP handler for connection tracking endpoints
func (a *API) HTTPHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/conntrack/connections", a.ListConnections)
	mux.HandleFunc("/api/v1/conntrack/stats", a.GetStats)
	return mux
}

// protocolToString converts protocol number to string
func protocolToString(proto uint8) string {
	switch proto {
	case 6:
		return "TCP"
	case 17:
		return "UDP"
	case 1:
		return "ICMP"
	default:
		return "UNKNOWN"
	}
}
