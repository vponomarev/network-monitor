package health

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestState_DefaultNotReady(t *testing.T) {
	s := NewState()
	if s.Ready() {
		t.Fatal("new State should not be ready by default")
	}
}

func TestState_SetCollectorReady(t *testing.T) {
	s := NewState()
	s.SetCollectorReady(true)
	if !s.Ready() {
		t.Fatal("expected ready after SetCollectorReady(true)")
	}
	s.SetCollectorReady(false)
	if s.Ready() {
		t.Fatal("expected not ready after SetCollectorReady(false)")
	}
}

func TestLivenessHandler_AlwaysOK(t *testing.T) {
	s := NewState() // not ready
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)

	s.LivenessHandler()(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("liveness must be 200 even when not ready, got %d", rec.Code)
	}
}

func TestReadinessHandler_NotReady503(t *testing.T) {
	s := NewState() // not ready
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ready", nil)

	s.ReadinessHandler()(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when not ready, got %d", rec.Code)
	}
}

func TestReadinessHandler_Ready200(t *testing.T) {
	s := NewState()
	s.SetCollectorReady(true)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ready", nil)

	s.ReadinessHandler()(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 when ready, got %d", rec.Code)
	}
}

// TestState_ConcurrentAccess exercises the atomic under -race.
func TestState_ConcurrentAccess(t *testing.T) {
	s := NewState()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); s.SetCollectorReady(true) }()
		go func() { defer wg.Done(); _ = s.Ready() }()
	}
	wg.Wait()
}
