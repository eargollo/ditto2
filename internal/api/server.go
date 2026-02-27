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
	"github.com/eargollo/ditto/internal/scan"
	"github.com/eargollo/ditto/internal/scheduler"
	"github.com/eargollo/ditto/internal/trash"
)

// Server holds the HTTP server and all handler dependencies.
type Server struct {
	addr string
	srv  *http.Server
}

// New wires all routes and returns a Server ready to Run.
// readDB is an optional read-only connection pool; when provided, page-handler
// SELECT queries run through it so they don't contend with scan writes.
func New(
	addr string,
	db *sql.DB,
	readDB *sql.DB,
	cfg *config.Config,
	mgr *scan.Manager,
	trashMgr *trash.Manager,
	sched *scheduler.Scheduler,
	version string,
	templatesFS fs.FS,
	staticFS fs.FS,
) *Server {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)

	statusH := &handlers.StatusHandler{DB: db, Manager: mgr, Sched: sched, Version: version}
	scansH := &handlers.ScansHandler{DB: db, Manager: mgr}
	groupsH := &handlers.GroupsHandler{
		DB:      db,
		Trash:   trashMgr,
		Cfg:     cfg,
		ScanMgr: mgr,
	}
	filesH := &handlers.FilesHandler{DB: db}
	trashH := &handlers.TrashHandler{DB: db, Trash: trashMgr}
	statsH := &handlers.StatsHandler{DB: db}
	configH := &handlers.ConfigHandler{DB: db, Cfg: cfg, Manager: mgr}

	r.Route("/api", func(r chi.Router) {
		r.Get("/status", statusH.ServeHTTP)

		r.Post("/scans", scansH.Create)
		r.Get("/scans", scansH.List)
		r.Get("/scans/{id}/telemetry", scansH.Telemetry)
		r.Get("/scans/{id}", scansH.Get)
		r.Delete("/scans/current", scansH.Cancel)

		r.Get("/groups", groupsH.List)
		r.Get("/groups/{id}", groupsH.Get)
		r.Post("/groups/{id}/delete", groupsH.Delete)
		r.Post("/groups/{id}/ignore", groupsH.Ignore)
		r.Post("/groups/{id}/reset", groupsH.Reset)
		r.Get("/groups/{id}/thumbnail", groupsH.Thumbnail)

		r.Get("/files/{id}/info", filesH.Info)
		r.Get("/files/{id}/thumbnail", filesH.Thumbnail)
		r.Get("/files/{id}/preview", filesH.Preview)

		r.Get("/trash", trashH.List)
		r.Post("/trash/{id}/restore", trashH.Restore)
		r.Delete("/trash", trashH.PurgeAll)

		r.Get("/stats", statsH.ServeHTTP)

		r.Get("/config", configH.Get)
		r.Patch("/config", configH.Update)
	})

	if staticFS != nil {
		r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
	}
	if templatesFS != nil {
		rdb := readDB
		if rdb == nil {
			rdb = db
		}
		ps := &pageServer{
			db:          db,
			readDB:      rdb,
			mgr:         mgr,
			trashMgr:    trashMgr,
			cfg:         cfg,
			sched:       sched,
			templatesFS: templatesFS,
			cfgH:        configH,
		}
		r.Get("/", ps.dashboardPage)
		r.Get("/groups-ui", ps.groupsPage)
		r.Get("/groups-ui/{id}", ps.groupDetailPage)
		r.Get("/trash-ui", ps.trashPage)
		r.Get("/settings-ui", ps.settingsPage)

		// Fragment endpoints (HTMX polling)
		r.Get("/ui/scan-status", ps.scanStatusFragment)

		// UI action endpoints (form POST → redirect)
		r.Post("/ui/scan", ps.uiScanStart)
		r.Post("/ui/scan/cancel", ps.uiScanCancel)
		r.Post("/ui/groups/{id}/delete", ps.uiGroupDelete)
		r.Post("/ui/groups/{id}/ignore", ps.uiGroupIgnore)
		r.Post("/ui/groups/{id}/reset", ps.uiGroupReset)
		r.Post("/ui/trash/{id}/restore", ps.uiTrashRestore)
		r.Post("/ui/trash/purge", ps.uiTrashPurge)
		r.Post("/ui/settings", ps.uiSettingsSave)
	}

	return &Server{
		addr: addr,
		srv:  &http.Server{Addr: addr, Handler: r},
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
