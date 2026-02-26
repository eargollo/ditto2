package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"

	"github.com/eargollo/ditto/internal/config"
	"github.com/eargollo/ditto/internal/db"
	"github.com/eargollo/ditto/internal/scan"
)

// ConfigHandler handles GET/PATCH /api/config.
type ConfigHandler struct {
	DB      *sql.DB
	Cfg     *config.Config
	Manager *scan.Manager
	mu      sync.Mutex // guards Cfg mutations
}

// ConfigPatch describes the fields that can be updated at runtime.
// Only supplied (non-nil) fields are applied.
type ConfigPatch struct {
	ScanPaths          []string     `json:"scan_paths"`
	ExcludePaths       []string     `json:"exclude_paths"`
	Schedule           *string      `json:"schedule"`
	ScanPaused         *bool        `json:"scan_paused"`
	TrashRetentionDays *int         `json:"trash_retention_days"`
	ScanWorkers        *WorkerPatch `json:"scan_workers"`
}

// WorkerPatch holds optional updates for scan worker counts.
type WorkerPatch struct {
	Walkers        *int `json:"walkers"`
	PartialHashers *int `json:"partial_hashers"`
	FullHashers    *int `json:"full_hashers"`
}

// Get handles GET /api/config.
func (h *ConfigHandler) Get(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	writeJSON(w, http.StatusOK, h.Cfg)
}

// Apply acquires the config lock, applies each non-nil patch field to h.Cfg,
// persists each change to the settings table, and propagates to scan.Manager.
func (h *ConfigHandler) Apply(_ context.Context, patch ConfigPatch) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if patch.ScanPaths != nil {
		h.Cfg.ScanPaths = patch.ScanPaths
		if b, err := json.Marshal(patch.ScanPaths); err == nil {
			db.SaveSetting(h.DB, "scan_paths", string(b))
		}
	}
	if patch.ExcludePaths != nil {
		h.Cfg.ExcludePaths = patch.ExcludePaths
		if b, err := json.Marshal(patch.ExcludePaths); err == nil {
			db.SaveSetting(h.DB, "exclude_paths", string(b))
		}
	}
	if patch.Schedule != nil {
		h.Cfg.Schedule = *patch.Schedule
		db.SaveSetting(h.DB, "schedule", *patch.Schedule)
	}
	if patch.ScanPaused != nil {
		h.Cfg.ScanPaused = *patch.ScanPaused
		db.SaveSetting(h.DB, "scan_paused", strconv.FormatBool(*patch.ScanPaused))
	}
	if patch.TrashRetentionDays != nil {
		v := *patch.TrashRetentionDays
		if v < 1 || v > 365 {
			return fmt.Errorf("trash_retention_days must be 1–365")
		}
		h.Cfg.TrashRetentionDays = v
		db.SaveSetting(h.DB, "trash_retention_days", strconv.Itoa(v))
	}
	if patch.ScanWorkers != nil {
		if patch.ScanWorkers.Walkers != nil {
			h.Cfg.ScanWorkers.Walkers = *patch.ScanWorkers.Walkers
			db.SaveSetting(h.DB, "walkers", strconv.Itoa(*patch.ScanWorkers.Walkers))
		}
		if patch.ScanWorkers.PartialHashers != nil {
			h.Cfg.ScanWorkers.PartialHashers = *patch.ScanWorkers.PartialHashers
			db.SaveSetting(h.DB, "partial_hashers", strconv.Itoa(*patch.ScanWorkers.PartialHashers))
		}
		if patch.ScanWorkers.FullHashers != nil {
			h.Cfg.ScanWorkers.FullHashers = *patch.ScanWorkers.FullHashers
			db.SaveSetting(h.DB, "full_hashers", strconv.Itoa(*patch.ScanWorkers.FullHashers))
		}
	}

	// Propagate updated roots/excludes/workers to the scan manager.
	if h.Manager != nil {
		scanCfg := scan.Config{
			Walkers:        h.Cfg.ScanWorkers.Walkers,
			PartialHashers: h.Cfg.ScanWorkers.PartialHashers,
			FullHashers:    h.Cfg.ScanWorkers.FullHashers,
			BatchSize:      1000,
		}
		h.Manager.UpdateConfig(h.Cfg.ScanPaths, h.Cfg.ExcludePaths, scanCfg)
	}

	return nil
}

// Update handles PATCH /api/config.
func (h *ConfigHandler) Update(w http.ResponseWriter, r *http.Request) {
	var patch ConfigPatch
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid JSON body")
		return
	}

	if err := h.Apply(r.Context(), patch); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_CONFIG", err.Error())
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	writeJSON(w, http.StatusOK, h.Cfg)
}
