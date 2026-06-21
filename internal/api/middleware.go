package api

import (
	"net/http"
	"runtime/debug"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/ijaihundal/ctrlroom/internal/auth"
	"github.com/ijaihundal/ctrlroom/internal/logging"
)

// loggingMiddleware logs each HTTP request as a structured slog record.
func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		duration := time.Since(start)

		attrs := []any{
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"bytes", ww.BytesWritten(),
			"duration_ms", duration.Milliseconds(),
			"req_id", logging.ReqIDFromCtx(r.Context()),
			"remote", r.RemoteAddr,
		}
		if u := auth.UserFromCtx(r.Context()); u != nil {
			attrs = append(attrs, "user", u.Username)
		}
		s.logger.Info("http", attrs...)
	})
}

// recoverMiddleware catches panics, logs them with stack, returns 500.
func (s *Server) recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.logger.Error("panic",
					"recover", rec,
					"stack", string(debug.Stack()),
					"method", r.Method, "path", r.URL.Path,
					"req_id", logging.ReqIDFromCtx(r.Context()),
				)
				writeError(w, r, http.StatusInternalServerError, "internal", "Internal server error", nil)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// Authed requires authentication (cookie session OR Bearer api token).
// On failure responds 401. On success attaches *types.User to the request context.
func (s *Server) Authed(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, err := auth.Authenticate(r.Context(), r, s.db)
		if err != nil {
			unauthorized(w, r, "Authentication required")
			return
		}
		next.ServeHTTP(w, r.WithContext(auth.WithUser(r.Context(), u)))
	})
}
