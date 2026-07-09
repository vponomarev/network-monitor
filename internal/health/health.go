// Package health tracks the runtime readiness of netmon components and exposes
// liveness / readiness HTTP handlers.
//
// Liveness (/health) answers "is the process alive and serving HTTP" — it is
// always 200 while the server runs. Readiness (/ready) answers "is netmon
// actually collecting data" — it is 200 only once the loss collector has
// started successfully, and flips back to 503 when the collector stops.
//
// The readiness signal is component-agnostic: whichever loss collector is
// active (trace_pipe today, eBPF later) calls SetCollectorReady. This keeps
// the health surface stable across the collector migration.
package health

import (
	"net/http"
	"sync/atomic"
)

// State holds the readiness of netmon components. It is safe for concurrent use.
type State struct {
	// collectorReady is true once the loss collector has successfully started
	// and false before startup or after it stops.
	collectorReady atomic.Bool
}

// NewState returns a State with all components not-ready.
func NewState() *State {
	return &State{}
}

// SetCollectorReady records whether the loss collector is currently running.
func (s *State) SetCollectorReady(ready bool) {
	s.collectorReady.Store(ready)
}

// Ready reports whether netmon is ready to serve (loss collector running).
func (s *State) Ready() bool {
	return s.collectorReady.Load()
}

// LivenessHandler answers liveness probes. It is always 200 while the process
// serves HTTP — reaching this handler proves the process is alive.
func (s *State) LivenessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}
}

// ReadinessHandler answers readiness probes: 200 when the loss collector is
// running, 503 otherwise so orchestrators keep the instance out of rotation
// until it is actually collecting data.
func (s *State) ReadinessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !s.Ready() {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"not ready","reason":"loss collector not started"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	}
}
