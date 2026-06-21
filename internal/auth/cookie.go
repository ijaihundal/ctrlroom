package auth

import (
	"context"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ijaihundal/ctrlroom/internal/config"
	"github.com/ijaihundal/ctrlroom/internal/db"
	"github.com/ijaihundal/ctrlroom/internal/types"
)

const (
	CookieName   = "ctrlroom_sess"
	tokenBytes   = 32
	bearerScheme = "Bearer "
)

var (
	ErrNoSession     = errors.New("no session")
	ErrInvalidBearer = errors.New("invalid bearer token")
)

func Issue(
	ctx context.Context, w http.ResponseWriter, r *http.Request,
	cfg *config.Config, database *sql.DB, userID string,
) (string, error) {
	raw := make([]byte, tokenBytes)
	if _, err := cryptorand.Read(raw); err != nil {
		return "", fmt.Errorf("read token: %w", err)
	}
	rawToken := hex.EncodeToString(raw)
	hash := HashToken(rawToken)

	expiresAt := time.Now().Add(cfg.SessionTTL)
	if _, err := db.CreateSession(ctx, database, hash, userID, expiresAt); err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    rawToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   cfg.Env == "prod",
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(cfg.SessionTTL.Seconds()),
	})
	return rawToken, nil
}

func Clear(ctx context.Context, w http.ResponseWriter, r *http.Request, database *sql.DB) {
	if c, err := r.Cookie(CookieName); err == nil {
		_ = db.DeleteSession(ctx, database, HashToken(c.Value))
	}
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}

func Authenticate(ctx context.Context, r *http.Request, database *sql.DB) (*types.User, error) {
	if c, err := r.Cookie(CookieName); err == nil {
		return lookupBySessionToken(ctx, database, c.Value)
	}
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, bearerScheme) {
		return lookupByBearer(ctx, database, strings.TrimPrefix(h, bearerScheme))
	}
	return nil, ErrNoSession
}

func lookupBySessionToken(ctx context.Context, database *sql.DB, rawToken string) (*types.User, error) {
	session, err := db.LookupSession(ctx, database, HashToken(rawToken))
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil, ErrNoSession
		}
		return nil, fmt.Errorf("lookup session: %w", err)
	}
	u, err := db.GetUserByID(ctx, database, session.UserID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil, ErrNoSession
		}
		return nil, fmt.Errorf("lookup user: %w", err)
	}
	return u, nil
}

func lookupByBearer(ctx context.Context, database *sql.DB, rawToken string) (*types.User, error) {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return nil, ErrInvalidBearer
	}
	tok, err := db.LookupAPITokenByHash(ctx, database, HashToken(rawToken))
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil, ErrInvalidBearer
		}
		return nil, fmt.Errorf("lookup api token: %w", err)
	}
	u, err := db.GetUserByID(ctx, database, tok.UserID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil, ErrInvalidBearer
		}
		return nil, fmt.Errorf("lookup user: %w", err)
	}
	return u, nil
}

func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func ConstantTimeTokenCompare(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
