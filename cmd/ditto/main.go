package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/eargollo/ditto/internal/api"
	"github.com/eargollo/ditto/internal/config"
	"github.com/eargollo/ditto/internal/db"
	"github.com/eargollo/ditto/internal/scheduler"
	"github.com/eargollo/ditto/web"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	// ── Logging ────────────────────────────────────────────────────────────
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	// ── Config ─────────────────────────────────────────────────────────────
	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}

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

	// ── Settings overlay ───────────────────────────────────────────────────
	// TODO: load settings from DB and call config.MergeDBSettings(cfg, settings)

	// ── Scheduler ──────────────────────────────────────────────────────────
	sched := scheduler.New()
	if !cfg.ScanPaused && cfg.Schedule != "" {
		if err := sched.SetJob(cfg.Schedule, func() {
			slog.Info("scheduled scan triggered")
			// TODO: trigger scanner
		}); err != nil {
			slog.Warn("scheduler: invalid cron expression", "expr", cfg.Schedule, "error", err)
		}
	}
	sched.Start()
	defer sched.Stop()

	// ── HTTP server ────────────────────────────────────────────────────────
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt, syscall.SIGTERM,
	)
	defer stop()

	srv := api.New(cfg.HTTPAddr, database, cfg, web.Templates(), web.Static())
	if err := srv.Run(ctx); err != nil {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
	slog.Info("ditto stopped")
}
