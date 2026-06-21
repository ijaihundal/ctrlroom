package types

import "time"

type APIToken struct {
	ID          string    `json:"id"`
	UserID      string    `json:"user_id"`
	WorkspaceID *string   `json:"workspace_id,omitempty"`
	TokenHash   string    `json:"-"`
	ExpiresAt   time.Time `json:"expires_at"`
	CreatedAt   time.Time `json:"created_at"`
}
