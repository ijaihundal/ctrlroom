package testutil

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

// DoJSON sends a JSON request and returns the raw response.
// body may be nil for GET/DELETE; otherwise it is marshaled as JSON.
// cookie may be nil for unauthenticated requests.
// bearerToken may be "" to skip the Authorization header.
// The caller is responsible for closing resp.Body.
func DoJSON(t *testing.T, method, url string, body any, cookie *http.Cookie, bearerToken string) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

// DecodeJSON decodes a JSON response body into dst. Closes the body.
func DecodeJSON(t *testing.T, r io.Reader, dst any) {
	t.Helper()
	if err := json.NewDecoder(r).Decode(dst); err != nil {
		t.Fatalf("decode json: %v", err)
	}
}

// AssertStatus fails the test if resp.StatusCode != want.
func AssertStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode != want {
		t.Fatalf("status %d, want %d", resp.StatusCode, want)
	}
}
