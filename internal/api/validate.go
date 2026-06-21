package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

const maxBodyBytes = 1 << 20 // 1 MiB

// ErrBodyEmpty is returned by decodeJSON when the request body is empty.
var ErrBodyEmpty = errors.New("request body is empty")

// ErrBodyTooLarge is returned by decodeJSON when the request body exceeds maxBodyBytes.
var ErrBodyTooLarge = errors.New("request body exceeds 1 MiB limit")

// decodeJSON decodes a size-limited JSON body into dst.
// Returns ErrBodyEmpty if the body is empty, ErrBodyTooLarge if it exceeds 1 MiB.
func decodeJSON(r *http.Request, dst any) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes+1))
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}
	if len(body) == 0 {
		return ErrBodyEmpty
	}
	if len(body) > maxBodyBytes {
		return ErrBodyTooLarge
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	return nil
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, payload any) error {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		return fmt.Errorf("encode: %w", err)
	}
	return nil
}
