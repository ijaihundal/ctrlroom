package auth

import (
	"context"
	cryptorand "crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ijaihundal/ctrlroom/internal/config"
	"github.com/ijaihundal/ctrlroom/internal/db"
	"github.com/ijaihundal/ctrlroom/internal/types"
)

func setup(t *testing.T) (*sql.DB, *types.User, *config.Config) {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Apply(context.Background(), database); err != nil {
		t.Fatalf("apply: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	cfg := &config.Config{
		Env:           "dev",
		SessionTTL:    time.Hour,
		Argon2Memory:  1024,
		Argon2Time:    1,
		Argon2Threads: 1,
	}
	hash, err := Hash("password123", cfg)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	user, err := db.CreateUser(context.Background(), database, "alice", hash)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return database, user, cfg
}

func seedAPIToken(t *testing.T, database *sql.DB, userID string) string {
	t.Helper()
	raw := make([]byte, tokenBytes)
	if _, err := cryptorand.Read(raw); err != nil {
		t.Fatalf("read token: %v", err)
	}
	rawToken := hex.EncodeToString(raw)
	if _, err := db.CreateAPIToken(
		context.Background(), database, userID,
		HashToken(rawToken), nil, time.Now().Add(time.Hour),
	); err != nil {
		t.Fatalf("create api token: %v", err)
	}
	return rawToken
}

func requestWithCookie(cookie *http.Cookie) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	return r
}

func TestIssue_SetsCookieAttrsAndPersistsRow(t *testing.T) {
	t.Parallel()
	database, user, cfg := setup(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	raw, err := Issue(context.Background(), w, r, cfg, database, user.ID)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if raw == "" {
		t.Fatal("raw token is empty")
	}

	resp := w.Result()
	defer resp.Body.Close()
	cookies := resp.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("got %d cookies, want 1", len(cookies))
	}
	c := cookies[0]
	if c.Name != CookieName {
		t.Errorf("cookie name = %q, want %q", c.Name, CookieName)
	}
	if c.Value != raw {
		t.Errorf("cookie value = %q, want raw token", c.Value)
	}
	if !c.HttpOnly {
		t.Error("cookie is not HttpOnly")
	}
	if c.SameSite != http.SameSiteStrictMode {
		t.Errorf("SameSite = %v, want Strict", c.SameSite)
	}
	if c.Path != "/" {
		t.Errorf("Path = %q, want /", c.Path)
	}
	if c.Secure {
		t.Error("Secure should be false in dev env")
	}
	if c.MaxAge != int(cfg.SessionTTL.Seconds()) {
		t.Errorf("MaxAge = %d, want %d", c.MaxAge, int(cfg.SessionTTL.Seconds()))
	}

	stored, err := db.LookupSession(context.Background(), database, HashToken(raw))
	if err != nil {
		t.Fatalf("lookup session: %v", err)
	}
	if stored.UserID != user.ID {
		t.Errorf("session user = %q, want %q", stored.UserID, user.ID)
	}
}

func TestIssue_ProdSetsSecure(t *testing.T) {
	t.Parallel()
	database, user, cfg := setup(t)
	cfg.Env = "prod"

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	raw, err := Issue(context.Background(), w, r, cfg, database, user.ID)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	resp := w.Result()
	defer resp.Body.Close()
	c := resp.Cookies()[0]
	if !c.Secure {
		t.Error("Secure should be true in prod env")
	}
	if c.Value != raw {
		t.Errorf("cookie value mismatch")
	}
}

func TestAuthenticate_WithValidCookieReturnsUser(t *testing.T) {
	t.Parallel()
	database, user, cfg := setup(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	if _, err := Issue(context.Background(), w, r, cfg, database, user.ID); err != nil {
		t.Fatalf("issue: %v", err)
	}

	resp := w.Result()
	defer resp.Body.Close()
	authReq := requestWithCookie(resp.Cookies()[0])
	got, err := Authenticate(context.Background(), authReq, database)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if got.ID != user.ID {
		t.Errorf("user id = %q, want %q", got.ID, user.ID)
	}
}

func TestAuthenticate_NoCredentialsReturnsErrNoSession(t *testing.T) {
	t.Parallel()
	database, _, _ := setup(t)

	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	_, err := Authenticate(context.Background(), r, database)
	if !errors.Is(err, ErrNoSession) {
		t.Errorf("err = %v, want ErrNoSession", err)
	}
}

func TestAuthenticate_TamperedCookieReturnsErrNoSession(t *testing.T) {
	t.Parallel()
	database, user, cfg := setup(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	if _, err := Issue(context.Background(), w, r, cfg, database, user.ID); err != nil {
		t.Fatalf("issue: %v", err)
	}
	resp := w.Result()
	resp.Body.Close()

	tampered := &http.Cookie{Name: CookieName, Value: "not-a-real-token"}
	authReq := requestWithCookie(tampered)
	_, err := Authenticate(context.Background(), authReq, database)
	if !errors.Is(err, ErrNoSession) {
		t.Errorf("err = %v, want ErrNoSession", err)
	}
}

func TestAuthenticate_BearerValidTokenReturnsUser(t *testing.T) {
	t.Parallel()
	database, user, _ := setup(t)
	raw := seedAPIToken(t, database, user.ID)

	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	r.Header.Set("Authorization", bearerScheme+raw)
	got, err := Authenticate(context.Background(), r, database)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if got.ID != user.ID {
		t.Errorf("user id = %q, want %q", got.ID, user.ID)
	}
}

func TestAuthenticate_BearerGarbageReturnsErrInvalidBearer(t *testing.T) {
	t.Parallel()
	database, _, _ := setup(t)

	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	r.Header.Set("Authorization", bearerScheme+"garbage-token")
	_, err := Authenticate(context.Background(), r, database)
	if !errors.Is(err, ErrInvalidBearer) {
		t.Errorf("err = %v, want ErrInvalidBearer", err)
	}
}

func TestAuthenticate_BearerEmptyReturnsErrInvalidBearer(t *testing.T) {
	t.Parallel()
	database, _, _ := setup(t)

	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	r.Header.Set("Authorization", bearerScheme+"   ")
	_, err := Authenticate(context.Background(), r, database)
	if !errors.Is(err, ErrInvalidBearer) {
		t.Errorf("err = %v, want ErrInvalidBearer", err)
	}
}

func TestClear_DeletesSessionAndExpiresCookie(t *testing.T) {
	t.Parallel()
	database, user, cfg := setup(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	raw, err := Issue(context.Background(), w, r, cfg, database, user.ID)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	issueResp := w.Result()
	issueResp.Body.Close()
	cookie := issueResp.Cookies()[0]

	clearReq := requestWithCookie(cookie)
	clearW := httptest.NewRecorder()
	Clear(context.Background(), clearW, clearReq, database)

	_, err = db.LookupSession(context.Background(), database, HashToken(raw))
	if !errors.Is(err, db.ErrNotFound) {
		t.Errorf("after clear, lookup err = %v, want ErrNotFound", err)
	}

	clearResp := clearW.Result()
	defer clearResp.Body.Close()
	expired := clearResp.Cookies()
	if len(expired) != 1 {
		t.Fatalf("got %d cookies after clear, want 1", len(expired))
	}
	c := expired[0]
	if c.Name != CookieName {
		t.Errorf("cookie name = %q, want %q", c.Name, CookieName)
	}
	if c.MaxAge != -1 {
		t.Errorf("MaxAge = %d, want -1", c.MaxAge)
	}
	if !c.Expires.Before(time.Now()) {
		t.Errorf("Expires = %v, want in the past", c.Expires)
	}
}

func TestClear_NoCookieStillExpiresHeader(t *testing.T) {
	t.Parallel()
	database, _, _ := setup(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	Clear(context.Background(), w, r, database)

	resp := w.Result()
	defer resp.Body.Close()
	cookies := resp.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("got %d cookies, want 1", len(cookies))
	}
	if cookies[0].MaxAge != -1 {
		t.Errorf("MaxAge = %d, want -1", cookies[0].MaxAge)
	}
}

func TestHashToken_Deterministic(t *testing.T) {
	t.Parallel()
	in := "abc123"
	a := HashToken(in)
	b := HashToken(in)
	if a != b {
		t.Errorf("HashToken not deterministic: %q != %q", a, b)
	}
	if len(a) != 64 {
		t.Errorf("HashToken len = %d, want 64 (sha256 hex)", len(a))
	}
}

func TestConstantTimeTokenCompare(t *testing.T) {
	t.Parallel()
	if !ConstantTimeTokenCompare("xyz", "xyz") {
		t.Error("equal strings returned false")
	}
	if ConstantTimeTokenCompare("xyz", "abc") {
		t.Error("different strings returned true")
	}
}

func TestContext_UserRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	if u := UserFromCtx(ctx); u != nil {
		t.Errorf("empty ctx UserFromCtx = %v, want nil", u)
	}
	if _, err := RequireUser(ctx); err == nil {
		t.Error("RequireUser on empty ctx returned nil error")
	}

	want := &types.User{ID: "u-1", Username: "bob"}
	ctx = WithUser(ctx, want)
	got := UserFromCtx(ctx)
	if got != want {
		t.Errorf("UserFromCtx = %v, want %v", got, want)
	}
	req, err := RequireUser(ctx)
	if err != nil {
		t.Fatalf("RequireUser: %v", err)
	}
	if req != want {
		t.Errorf("RequireUser = %v, want %v", req, want)
	}
}
