package api

import (
	"database/sql"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/ijaihundal/ctrlroom/internal/config"
	"github.com/ijaihundal/ctrlroom/internal/git"
	"github.com/ijaihundal/ctrlroom/internal/logging"
	"github.com/ijaihundal/ctrlroom/internal/workspace"
)

type Server struct {
	cfg          *config.Config
	db           *sql.DB
	logger       *slog.Logger
	gitClient    *git.Client
	workspaceMgr *workspace.Manager
}

func New(
	cfg *config.Config,
	database *sql.DB,
	logger *slog.Logger,
	gitClient *git.Client,
	workspaceMgr *workspace.Manager,
) *Server {
	return &Server{
		cfg:          cfg,
		db:           database,
		logger:       logger,
		gitClient:    gitClient,
		workspaceMgr: workspaceMgr,
	}
}

// Handler returns the configured HTTP handler. Mount this directly on http.Server.
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID) // populates chi's request-id context key
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if reqID := middleware.GetReqID(r.Context()); reqID != "" {
				r = r.WithContext(logging.WithReqID(r.Context(), reqID))
			}
			next.ServeHTTP(w, r)
		})
	})
	r.Use(middleware.RealIP)   // parse X-Forwarded-For / X-Real-IP
	r.Use(s.recoverMiddleware) // panic recovery
	r.Use(s.loggingMiddleware) // structured access log

	r.Route("/api", func(r chi.Router) {
		r.Get("/health", s.handleHealth)
		r.Post("/auth/login", s.handleLogin)

		r.Group(func(r chi.Router) {
			r.Use(s.Authed)

			r.Post("/auth/logout", s.handleLogout)
			r.Get("/auth/me", s.handleMe)

			r.Route("/projects", func(r chi.Router) {
				r.Get("/", s.handleListProjects)
				r.Post("/", s.handleCreateProject)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", s.handleGetProject)
					r.Patch("/", s.handleUpdateProject)
					r.Delete("/", s.handleDeleteProject)
					r.Get("/issues", s.handleListIssues)
					r.Post("/issues", s.handleCreateIssue)
					r.Post("/issues/reorder", s.handleReorderIssues)
					r.Get("/workspaces", s.handleListWorkspacesByProject)
				})
			})

			r.Route("/issues", func(r chi.Router) {
				r.Patch("/{id}", s.handleUpdateIssue)
				r.Delete("/{id}", s.handleDeleteIssue)
			})

			r.Route("/workspaces", func(r chi.Router) {
				r.Post("/", s.handleCreateWorkspace)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", s.handleGetWorkspace)
					r.Get("/diff", s.handleDiff)
					r.Post("/stop", s.handleStopWorkspace)
					r.Post("/merge", s.handleMergeWorkspace)
					r.Post("/message", s.handleMessage)
				})
			})
		})
	})

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		notFound(w, r, "Route not found")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed", nil)
	})

	return r
}
