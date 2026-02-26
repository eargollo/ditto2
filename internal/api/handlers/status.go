package handlers

import (
	"database/sql"
	"log/slog"
	"net/http"
	"time"

	"github.com/eargollo/ditto/internal/scan"
	"github.com/eargollo/ditto/internal/scheduler"
)

// StatusHandler handles GET /api/status.
type StatusHandler struct {
	DB      *sql.DB
	Manager *scan.Manager
	Sched   *scheduler.Scheduler
	Version string
}

type statusResponse struct {
	Version           string             `json:"version"`
	ActiveScan        *activeScanInfo    `json:"active_scan"`
	Schedule          scheduleInfo       `json:"schedule"`
	LastCompletedScan *completedScanInfo `json:"last_completed_scan"`
}

type activeScanInfo struct {
	ID          int64            `json:"id"`
	StartedAt   string           `json:"started_at"`
	TriggeredBy string           `json:"triggered_by"`
	Progress    scanProgressInfo `json:"progress"`
}

type scanProgressInfo struct {
	FilesDiscovered int64 `json:"files_discovered"`
	CandidatesFound int64 `json:"candidates_found"`
	PartialHashed   int64 `json:"partial_hashed"`
	FullHashed      int64 `json:"full_hashed"`
	BytesRead       int64 `json:"bytes_read"`
	CacheHits       int64 `json:"cache_hits"`
	CacheMisses     int64 `json:"cache_misses"`
}

type scheduleInfo struct {
	Cron      string  `json:"cron"`
	Paused    bool    `json:"paused"`
	NextRunAt *string `json:"next_run_at"`
}

type completedScanInfo struct {
	ID               int64   `json:"id"`
	FinishedAt       string  `json:"finished_at"`
	DuplicateGroups  int64   `json:"duplicate_groups"`
	DuplicateFiles   int64   `json:"duplicate_files"`
	ReclaimableBytes int64   `json:"reclaimable_bytes"`
	CacheHits        int64   `json:"cache_hits"`
	CacheMisses      int64   `json:"cache_misses"`
	CacheHitRate     float64 `json:"cache_hit_rate"`
}

// ServeHTTP returns the system status as JSON.
func (h *StatusHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	resp := statusResponse{
		Version:           h.Version,
		ActiveScan:        h.activeScan(),
		Schedule:          h.schedule(),
		LastCompletedScan: h.lastCompletedScan(),
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *StatusHandler) activeScan() *activeScanInfo {
	if h.Manager == nil {
		return nil
	}
	a := h.Manager.ActiveScan()
	if a == nil {
		return nil
	}
	p := a.Progress
	return &activeScanInfo{
		ID:          a.ID,
		StartedAt:   a.StartedAt.UTC().Format(time.RFC3339),
		TriggeredBy: a.TriggeredBy,
		Progress: scanProgressInfo{
			FilesDiscovered: p.FilesDiscovered.Load(),
			CandidatesFound: p.CandidatesFound.Load(),
			PartialHashed:   p.PartialHashed.Load(),
			FullHashed:      p.FullHashed.Load(),
			BytesRead:       p.BytesRead.Load(),
			CacheHits:       p.CacheHits.Load(),
			CacheMisses:     p.CacheMisses.Load(),
		},
	}
}

func (h *StatusHandler) schedule() scheduleInfo {
	info := scheduleInfo{
		Cron:   "0 2 * * 0",
		Paused: false,
	}
	if h.Sched != nil {
		info.Cron = h.Sched.CronExpr()
		if t := h.Sched.NextRunAt(); t != nil {
			s := t.UTC().Format(time.RFC3339)
			info.NextRunAt = &s
		}
	}
	return info
}

func (h *StatusHandler) lastCompletedScan() *completedScanInfo {
	if h.DB == nil {
		return nil
	}
	row := h.DB.QueryRow(`
		SELECT id, finished_at, duplicate_groups, duplicate_files,
		       reclaimable_bytes, cache_hits, cache_misses
		FROM scan_history
		WHERE status = 'completed'
		ORDER BY finished_at DESC
		LIMIT 1`)

	var (
		id               int64
		finishedAt       int64
		duplicateGroups  int64
		duplicateFiles   int64
		reclaimableBytes int64
		cacheHits        int64
		cacheMisses      int64
	)
	err := row.Scan(&id, &finishedAt, &duplicateGroups, &duplicateFiles,
		&reclaimableBytes, &cacheHits, &cacheMisses)
	if err != nil {
		if err != sql.ErrNoRows {
			slog.Error("status: query last scan", "error", err)
		}
		return nil
	}
	var hitRate float64
	if total := cacheHits + cacheMisses; total > 0 {
		hitRate = float64(cacheHits) / float64(total)
	}
	return &completedScanInfo{
		ID:               id,
		FinishedAt:       time.Unix(finishedAt, 0).UTC().Format(time.RFC3339),
		DuplicateGroups:  duplicateGroups,
		DuplicateFiles:   duplicateFiles,
		ReclaimableBytes: reclaimableBytes,
		CacheHits:        cacheHits,
		CacheMisses:      cacheMisses,
		CacheHitRate:     hitRate,
	}
}
