package api

import (
	"context"
	"database/sql"
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/eargollo/ditto/internal/api/handlers"
	"github.com/eargollo/ditto/internal/config"
)

// Server holds the HTTP server and all handler dependencies.
type Server struct {
	addr string
	srv  *http.Server
}

// New wires all routes and returns a Server ready to Run.
// templatesFS and staticFS come from the web package (embed.FS sub-trees).
func New(addr string, db *sql.DB, cfg *config.Config, templatesFS fs.FS, staticFS fs.FS) *Server {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)

	// Handler instances
	statusH := &handlers.StatusHandler{DB: db}
	scansH := &handlers.ScansHandler{DB: db}
	groupsH := &handlers.GroupsHandler{DB: db}
	filesH := &handlers.FilesHandler{DB: db}
	trashH := &handlers.TrashHandler{DB: db}
	statsH := &handlers.StatsHandler{DB: db}
	configH := &handlers.ConfigHandler{DB: db, Cfg: cfg}

	// API routes
	r.Route("/api", func(r chi.Router) {
		r.Get("/status", statusH.ServeHTTP)

		r.Post("/scans", scansH.Create)
		r.Get("/scans", scansH.List)
		r.Get("/scans/{id}", scansH.Get)
		r.Delete("/scans/current", scansH.Cancel)

		r.Get("/groups", groupsH.List)
		r.Get("/groups/{id}", groupsH.Get)
		r.Post("/groups/{id}/delete", groupsH.Delete)
		r.Post("/groups/{id}/ignore", groupsH.Ignore)
		r.Get("/groups/{id}/thumbnail", groupsH.Thumbnail)

		r.Get("/files/{id}/thumbnail", filesH.Thumbnail)
		r.Get("/files/{id}/preview", filesH.Preview)

		r.Get("/trash", trashH.List)
		r.Post("/trash/{id}/restore", trashH.Restore)
		r.Delete("/trash", trashH.PurgeAll)

		r.Get("/stats", statsH.ServeHTTP)

		r.Get("/config", configH.Get)
		r.Patch("/config", configH.Update)
	})

	// Web UI — static assets
	if staticFS != nil {
		r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
	}

	// Web UI — HTML pages
	if templatesFS != nil {
		ph := newPageHandler(templatesFS)
		r.Get("/", ph("dashboard.html"))
		r.Get("/groups-ui", ph("groups.html"))
		r.Get("/groups-ui/{id}", ph("group_detail.html"))
		r.Get("/trash-ui", ph("trash.html"))
	}

	return &Server{
		addr: addr,
		srv: &http.Server{
			Addr:    addr,
			Handler: r,
		},
	}
}

// Run starts the HTTP server and blocks until ctx is cancelled.
func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		slog.Info("server listening", "addr", s.addr)
		if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		slog.Info("shutting down HTTP server")
		return s.srv.Shutdown(context.Background())
	case err := <-errCh:
		return err
	}
}
