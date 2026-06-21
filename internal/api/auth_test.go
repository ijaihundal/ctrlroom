package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ijaihundal/ctrlroom/internal/auth"
	"github.com/ijaihundal/ctrlroom/internal/db"
)

// newClient returns a fresh HTTP client with no cookie jar so tests cannot
// leak sessions to one another.
func newClient() *http.Client {
	return &http.Client{}
}

func doJSON(t *testing.T, client *http.Client, method, url string, body any) *http.Response {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, r)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

func doRaw(t *testing.T, client *http.Client, method, url, contentType, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

func decodeEnvelope(t *testing.T, body io.Reader) errorResponse {
	t.Helper()
	var env errorResponse
	if err := json.NewDecoder(body).Decode(&env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	return env
}

func extractSessionCookie(t *testing.T, resp *http.Response) *http.Cookie {
	t.Helper()
	for _, c := range resp.Cookies() {
		if c.Name == auth.CookieName {
			return c
		}
	}
	t.Fatalf("no %s cookie in response", auth.CookieName)
	return nil
}

func TestLoginSuccess(t *testing.T) {
	ts := setup(t)
	client := newClient()

	resp := doJSON(t, client, http.MethodPost, ts.server.URL+"/api/auth/login",
		map[string]string{"username": "admin", "password": ts.password})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	cookie := extractSessionCookie(t, resp)
	if cookie.Value == "" {
		t.Fatalf("session cookie value is empty")
	}

	var got loginResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got.User.Username != "admin" {
		t.Errorf("user.username = %q, want %q", got.User.Username, "admin")
	}
	if got.User.ID != ts.admin.ID {
		t.Errorf("user.id = %q, want %q", got.User.ID, ts.admin.ID)
	}
}

func TestLoginWrongPassword(t *testing.T) {
	ts := setup(t)
	client := newClient()

	resp := doJSON(t, client, http.MethodPost, ts.server.URL+"/api/auth/login",
		map[string]string{"username": "admin", "password": "wrong-password"})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
	for _, c := range resp.Cookies() {
		if c.Name == auth.CookieName {
			t.Errorf("unexpected Set-Cookie for %q", auth.CookieName)
		}
	}
	env := decodeEnvelope(t, resp.Body)
	if env.Error.Code != "unauthorized" {
		t.Errorf("code = %q, want %q", env.Error.Code, "unauthorized")
	}
}

func TestLoginUnknownUser(t *testing.T) {
	ts := setup(t)
	client := newClient()

	resp := doJSON(t, client, http.MethodPost, ts.server.URL+"/api/auth/login",
		map[string]string{"username": "no-such-user", "password": ts.password})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
	env := decodeEnvelope(t, resp.Body)
	if env.Error.Code != "unauthorized" {
		t.Errorf("code = %q, want %q", env.Error.Code, "unauthorized")
	}
	if !strings.Contains(strings.ToLower(env.Error.Message), "invalid") {
		t.Errorf("message = %q, want substring 'invalid'", env.Error.Message)
	}
}

func TestLoginMissingFields(t *testing.T) {
	ts := setup(t)
	client := newClient()

	cases := []struct {
		name string
		body any
	}{
		{"empty body", map[string]string{}},
		{"missing password", map[string]string{"username": "admin"}},
		{"missing username", map[string]string{"password": ts.password}},
		{"whitespace username", map[string]string{"username": "   ", "password": ts.password}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := doJSON(t, client, http.MethodPost, ts.server.URL+"/api/auth/login", tc.body)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
			}
			env := decodeEnvelope(t, resp.Body)
			if env.Error.Code != "missing_credentials" {
				t.Errorf("code = %q, want %q", env.Error.Code, "missing_credentials")
			}
		})
	}
}

func TestLoginMalformedJSON(t *testing.T) {
	ts := setup(t)
	client := newClient()

	resp := doRaw(t, client, http.MethodPost, ts.server.URL+"/api/auth/login",
		"application/json", "{not json")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
	env := decodeEnvelope(t, resp.Body)
	if env.Error.Code != "invalid_body" {
		t.Errorf("code = %q, want %q", env.Error.Code, "invalid_body")
	}
}

func TestMeWithoutCookie(t *testing.T) {
	ts := setup(t)
	client := newClient()

	resp, err := client.Get(ts.server.URL + "/api/auth/me")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
	env := decodeEnvelope(t, resp.Body)
	if env.Error.Code != "unauthorized" {
		t.Errorf("code = %q, want %q", env.Error.Code, "unauthorized")
	}
}

func TestMeWithValidCookie(t *testing.T) {
	ts := setup(t)
	client := newClient()

	login := doJSON(t, client, http.MethodPost, ts.server.URL+"/api/auth/login",
		map[string]string{"username": "admin", "password": ts.password})
	cookie := extractSessionCookie(t, login)
	login.Body.Close()

	req, err := http.NewRequest(http.MethodGet, ts.server.URL+"/api/auth/me", http.NoBody)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.AddCookie(cookie)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var got meResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got.User.ID != ts.admin.ID {
		t.Errorf("user.id = %q, want %q", got.User.ID, ts.admin.ID)
	}
	if got.User.Username != "admin" {
		t.Errorf("user.username = %q, want %q", got.User.Username, "admin")
	}
}

func TestMeWithTamperedCookie(t *testing.T) {
	ts := setup(t)
	client := newClient()

	login := doJSON(t, client, http.MethodPost, ts.server.URL+"/api/auth/login",
		map[string]string{"username": "admin", "password": ts.password})
	cookie := extractSessionCookie(t, login)
	login.Body.Close()

	cookie.Value += "deadbeef"

	req, err := http.NewRequest(http.MethodGet, ts.server.URL+"/api/auth/me", http.NoBody)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.AddCookie(cookie)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestLogoutWithValidCookie(t *testing.T) {
	ts := setup(t)
	client := newClient()

	login := doJSON(t, client, http.MethodPost, ts.server.URL+"/api/auth/login",
		map[string]string{"username": "admin", "password": ts.password})
	cookie := extractSessionCookie(t, login)
	login.Body.Close()

	// Logout
	req, err := http.NewRequest(http.MethodPost, ts.server.URL+"/api/auth/logout", http.NoBody)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.AddCookie(cookie)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do logout: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("logout status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}

	// Subsequent /me with the same (now-deleted) cookie must 401.
	req2, err := http.NewRequest(http.MethodGet, ts.server.URL+"/api/auth/me", http.NoBody)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req2.AddCookie(cookie)
	resp2, err := client.Do(req2)
	if err != nil {
		t.Fatalf("do me: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Errorf("post-logout me status = %d, want %d", resp2.StatusCode, http.StatusUnauthorized)
	}
}

func TestLogoutWithoutCookie(t *testing.T) {
	ts := setup(t)
	client := newClient()

	resp, err := client.Post(ts.server.URL+"/api/auth/logout", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("post logout: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestMeWithValidBearer(t *testing.T) {
	ts := setup(t)
	client := newClient()

	// Seed an api_token: generate raw bytes, store sha256 hash, keep raw for the header.
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		t.Fatalf("read rand: %v", err)
	}
	rawToken := hex.EncodeToString(raw)
	if _, err := db.CreateAPIToken(
		context.Background(), ts.database,
		ts.admin.ID, auth.HashToken(rawToken), nil, time.Now().Add(time.Hour),
	); err != nil {
		t.Fatalf("create api token: %v", err)
	}

	req, err := http.NewRequest(http.MethodGet, ts.server.URL+"/api/auth/me", http.NoBody)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+rawToken)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var got meResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got.User.ID != ts.admin.ID {
		t.Errorf("user.id = %q, want %q", got.User.ID, ts.admin.ID)
	}
}

func TestMeWithGarbageBearer(t *testing.T) {
	ts := setup(t)
	client := newClient()

	req, err := http.NewRequest(http.MethodGet, ts.server.URL+"/api/auth/me", http.NoBody)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer this-is-not-a-real-token")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestNotFoundRoute(t *testing.T) {
	ts := setup(t)
	client := newClient()

	resp, err := client.Get(ts.server.URL + "/api/nope")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
	env := decodeEnvelope(t, resp.Body)
	if env.Error.Code != "not_found" {
		t.Errorf("code = %q, want %q", env.Error.Code, "not_found")
	}
}
