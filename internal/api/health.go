package api

import (
	"context"
	"net/http"
	"time"

	"github.com/ijaihundal/ctrlroom/internal/version"
)

type healthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	DB      string `json:"db"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	status := "ok"
	dbStatus := "ok"

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.db.PingContext(ctx); err != nil {
		status = "degraded"
		dbStatus = "error"
	}

	_ = writeJSON(w, http.StatusOK, healthResponse{
		Status:  status,
		Version: version.String(),
		DB:      dbStatus,
	})
}
