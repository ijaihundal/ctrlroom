package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestRecoverMiddleware asserts that panics inside a handler are caught by
// recoverMiddleware and turned into a 500 with the standard error envelope.
func TestRecoverMiddleware(t *testing.T) {
	srv := &Server{logger: slog.Default()}
	mw := srv.recoverMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	mw.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	var env errorResponse
	if err := json.NewDecoder(rec.Body).Decode(&env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Error.Code != "internal" {
		t.Errorf("code = %q, want %q", env.Error.Code, "internal")
	}
}

// TestMethodNotAllowed asserts chi's MethodNotAllowed hook is wired up and
// returns our envelope.
func TestMethodNotAllowed(t *testing.T) {
	ts := setup(t)
	client := newClient()

	req, err := http.NewRequest(http.MethodPost, ts.server.URL+"/api/health", http.NoBody)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusMethodNotAllowed)
	}
	var env errorResponse
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Error.Code != "method_not_allowed" {
		t.Errorf("code = %q, want %q", env.Error.Code, "method_not_allowed")
	}
}
