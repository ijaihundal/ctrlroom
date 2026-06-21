package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/ijaihundal/ctrlroom/internal/logging"
)

type APIError struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

type errorResponse struct {
	Error APIError `json:"error"`
}

// writeError writes a structured error response and logs it.
// The request ID (if any) is included in the log fields.
func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string, details map[string]any) {
	resp := errorResponse{Error: APIError{Code: code, Message: message, Details: details}}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("failed to write error response", "err", err, "req_id", logging.ReqIDFromCtx(r.Context()))
	}
	slog.Warn("api error",
		"status", status, "code", code, "message", message,
		"method", r.Method, "path", r.URL.Path, "req_id", logging.ReqIDFromCtx(r.Context()),
	)
}

func badRequest(w http.ResponseWriter, r *http.Request, code, message string) {
	writeError(w, r, http.StatusBadRequest, code, message, nil)
}

func unauthorized(w http.ResponseWriter, r *http.Request, message string) {
	writeError(w, r, http.StatusUnauthorized, "unauthorized", message, nil)
}

func notFound(w http.ResponseWriter, r *http.Request, message string) {
	writeError(w, r, http.StatusNotFound, "not_found", message, nil)
}

func internalError(w http.ResponseWriter, r *http.Request, message string, err error) {
	details := map[string]any{}
	if err != nil {
		details["error"] = err.Error()
	}
	writeError(w, r, http.StatusInternalServerError, "internal", message, details)
}
