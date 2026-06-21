package types

import "time"

type Session struct {
	Token     string    `json:"-"`
	TokenHash string    `json:"-"`
	UserID    string    `json:"user_id"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}
