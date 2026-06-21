package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/ijaihundal/ctrlroom/internal/auth"
	"github.com/ijaihundal/ctrlroom/internal/db"
	"github.com/ijaihundal/ctrlroom/internal/types"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type publicUser struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	CreatedAt string `json:"created_at"` // RFC3339
}

type loginResponse struct {
	User publicUser `json:"user"`
}

type meResponse struct {
	User publicUser `json:"user"`
}

func toPublicUser(u *types.User) publicUser {
	return publicUser{
		ID:        u.ID,
		Username:  u.Username,
		CreatedAt: u.CreatedAt.Format(time.RFC3339),
	}
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, r, "invalid_body", "Request body could not be parsed: "+err.Error())
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || req.Password == "" {
		badRequest(w, r, "missing_credentials", "Username and password are required")
		return
	}

	user, err := db.GetUserByUsername(r.Context(), s.db, req.Username)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			// Do not leak user existence; same response as wrong password.
			unauthorized(w, r, "Invalid username or password")
			return
		}
		internalError(w, r, "Failed to look up user", err)
		return
	}

	ok, err := auth.Verify(user.PasswordHash, req.Password)
	if err != nil {
		internalError(w, r, "Failed to verify password", err)
		return
	}
	if !ok {
		unauthorized(w, r, "Invalid username or password")
		return
	}

	if _, err := auth.Issue(r.Context(), w, r, s.cfg, s.db, user.ID); err != nil {
		internalError(w, r, "Failed to create session", err)
		return
	}

	_ = writeJSON(w, http.StatusOK, loginResponse{User: toPublicUser(user)})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	auth.Clear(r.Context(), w, r, s.db)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	u, err := auth.RequireUser(r.Context())
	if err != nil {
		unauthorized(w, r, "Authentication required")
		return
	}
	_ = writeJSON(w, http.StatusOK, meResponse{User: toPublicUser(u)})
}
