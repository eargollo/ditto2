package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"sync"

	"github.com/eargollo/ditto/internal/config"
	"github.com/eargollo/ditto/internal/scan"
)

// ConfigHandler handles GET/PATCH /api/config.
type ConfigHandler struct {
	DB      *sql.DB
	Cfg     *config.Config
	Manager *scan.Manager
	mu      sync.Mutex // guards Cfg mutations
}

// configPatch describes the fields that can be updated at runtime.
// Only supplied (non-nil) fields are applied.
type configPatch struct {
	ScanPaths          []string     `json:"scan_paths"`
	ExcludePaths       []string     `json:"exclude_paths"`
	Schedule           *string      `json:"schedule"`
	ScanPaused         *bool        `json:"scan_paused"`
	TrashRetentionDays *int         `json:"trash_retention_days"`
	ScanWorkers        *workerPatch `json:"scan_workers"`
}

type workerPatch struct {
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

// Update handles PATCH /api/config.
func (h *ConfigHandler) Update(w http.ResponseWriter, r *http.Request) {
	var patch configPatch
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid JSON body")
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if patch.ScanPaths != nil {
		h.Cfg.ScanPaths = patch.ScanPaths
	}
	if patch.ExcludePaths != nil {
		h.Cfg.ExcludePaths = patch.ExcludePaths
	}
	if patch.Schedule != nil {
		h.Cfg.Schedule = *patch.Schedule
	}
	if patch.ScanPaused != nil {
		h.Cfg.ScanPaused = *patch.ScanPaused
	}
	if patch.TrashRetentionDays != nil {
		v := *patch.TrashRetentionDays
		if v < 1 || v > 365 {
			writeError(w, http.StatusBadRequest, "INVALID_CONFIG", "trash_retention_days must be 1–365")
			return
		}
		h.Cfg.TrashRetentionDays = v
	}
	if patch.ScanWorkers != nil {
		if patch.ScanWorkers.Walkers != nil {
			h.Cfg.ScanWorkers.Walkers = *patch.ScanWorkers.Walkers
		}
		if patch.ScanWorkers.PartialHashers != nil {
			h.Cfg.ScanWorkers.PartialHashers = *patch.ScanWorkers.PartialHashers
		}
		if patch.ScanWorkers.FullHashers != nil {
			h.Cfg.ScanWorkers.FullHashers = *patch.ScanWorkers.FullHashers
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

	writeJSON(w, http.StatusOK, h.Cfg)
}
