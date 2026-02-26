package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/eargollo/ditto/internal/api"
	"github.com/eargollo/ditto/internal/config"
	"github.com/eargollo/ditto/internal/db"
	"github.com/eargollo/ditto/internal/scan"
	"github.com/eargollo/ditto/internal/scheduler"
	"github.com/eargollo/ditto/internal/trash"
	"github.com/eargollo/ditto/web"
)

// Injected at build time via -ldflags; defaults to "dev".
var version = "dev"

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	// ── Logging (initial — overridden below once config is loaded) ─────────
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	// ── Config ─────────────────────────────────────────────────────────────
	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}

	// Re-configure logging with the level from config (default: info).
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: parseLogLevel(cfg.LogLevel),
	})))
	slog.Info("ditto starting",
		"version", version,
		"log_level", cfg.LogLevel,
		"http_addr", cfg.HTTPAddr,
		"db_path", cfg.DBPath,
		"scan_paths", cfg.ScanPaths)

	// ── Database ───────────────────────────────────────────────────────────
	database, err := db.Open(cfg.DBPath)
	if err != nil {
		slog.Error("open database", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	if err := db.RunMigrations(database); err != nil {
		slog.Error("run migrations", "error", err)
		os.Exit(1)
	}

	if dbSettings, err := db.LoadSettings(database); err == nil {
		config.MergeDBSettings(cfg, dbSettings)
	}

	// Mark any scans that were 'running' when last process exited as failed.
	if err := scan.MarkStaleScansFailed(database); err != nil {
		slog.Warn("mark stale scans", "error", err)
	}

	// ── Scan manager ───────────────────────────────────────────────────────
	scanCfg := scan.Config{
		Walkers:        cfg.ScanWorkers.Walkers,
		CacheCheckers:  cfg.ScanWorkers.CacheCheckers,
		PartialHashers: cfg.ScanWorkers.PartialHashers,
		FullHashers:    cfg.ScanWorkers.FullHashers,
		BatchSize:      1000,
	}
	mgr := scan.NewManager(database, cfg.ScanPaths, cfg.ExcludePaths, scanCfg)

	// ── Trash manager ──────────────────────────────────────────────────────
	trashMgr := trash.New(database, cfg.TrashDir)

	// ── Scheduler ──────────────────────────────────────────────────────────
	sched := scheduler.New()
	if !cfg.ScanPaused && cfg.Schedule != "" {
		if err := sched.SetJob(cfg.Schedule, func() {
			slog.Info("scheduled scan triggered")
			if _, err := mgr.Start(context.Background(), "schedule"); err != nil {
				slog.Warn("scheduled scan start", "error", err)
			}
		}); err != nil {
			slog.Warn("invalid cron expression", "expr", cfg.Schedule, "error", err)
		}
	}

	if err := sched.AddJob("0 3 * * *", func() {
		slog.Info("auto-purge triggered")
		if err := trashMgr.AutoPurge(context.Background()); err != nil {
			slog.Error("auto-purge failed", "error", err)
		}
	}); err != nil {
		slog.Warn("failed to register auto-purge job", "error", err)
	}

	sched.Start()
	defer sched.Stop()

	// ── HTTP server ────────────────────────────────────────────────────────
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := api.New(cfg.HTTPAddr, database, cfg, mgr, trashMgr, sched, version, web.Templates(), web.Static())
	if err := srv.Run(ctx); err != nil {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
	slog.Info("ditto stopped")
}

// parseLogLevel converts a config string ("debug", "info", "warn", "error")
// to its slog.Level equivalent. Unknown values default to Info.
func parseLogLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
