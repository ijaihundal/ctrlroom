package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestHealth(t *testing.T) {
	ts := setup(t)

	resp, err := http.Get(ts.server.URL + "/api/health")
	if err != nil {
		t.Fatalf("get health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var got healthResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got.Status != "ok" {
		t.Errorf("status = %q, want %q", got.Status, "ok")
	}
	if got.DB != "ok" {
		t.Errorf("db = %q, want %q", got.DB, "ok")
	}
	if !strings.HasPrefix(got.Version, "dev") {
		t.Errorf("version = %q, want prefix %q", got.Version, "dev")
	}
	if got.Version == "" {
		t.Errorf("version is empty")
	}
}
