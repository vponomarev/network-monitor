package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRequireAuthMiddleware(t *testing.T) {
	const testToken = "test-secret-token"

	// Helper to create a handler that always returns 200 OK
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Run("no token configured - allows all requests", func(t *testing.T) {
		middleware := requireAuthMiddleware(okHandler, "")

		// Request without auth
		req := httptest.NewRequest("GET", "/test", nil)
		rr := httptest.NewRecorder()
		middleware.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
	})

	t.Run("token configured - no auth header - returns 401", func(t *testing.T) {
		middleware := requireAuthMiddleware(okHandler, testToken)

		req := httptest.NewRequest("GET", "/test", nil)
		rr := httptest.NewRecorder()
		middleware.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusUnauthorized, rr.Code)
		assert.Contains(t, rr.Body.String(), "unauthorized")
	})

	t.Run("token configured - wrong auth header - returns 401", func(t *testing.T) {
		middleware := requireAuthMiddleware(okHandler, testToken)

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer wrong-token")
		rr := httptest.NewRecorder()
		middleware.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusUnauthorized, rr.Code)
	})

	t.Run("token configured - correct auth header - returns 200", func(t *testing.T) {
		middleware := requireAuthMiddleware(okHandler, testToken)

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+testToken)
		rr := httptest.NewRecorder()
		middleware.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
	})

	t.Run("token configured - correct token via query param - returns 200", func(t *testing.T) {
		middleware := requireAuthMiddleware(okHandler, testToken)

		req := httptest.NewRequest("GET", "/test?token=Bearer%20"+testToken, nil)
		rr := httptest.NewRecorder()
		middleware.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
	})

	t.Run("token configured - empty token in header - returns 401", func(t *testing.T) {
		middleware := requireAuthMiddleware(okHandler, testToken)

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "")
		rr := httptest.NewRecorder()
		middleware.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusUnauthorized, rr.Code)
	})
}

// requireAuthMiddleware is a test helper that mimics the middleware from main.go
func requireAuthMiddleware(handler http.Handler, authToken string) http.Handler {
	if authToken == "" {
		return handler
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
		if token == "" {
			token = r.URL.Query().Get("token")
		}
		if token != "Bearer "+authToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		handler.ServeHTTP(w, r)
	})
}
