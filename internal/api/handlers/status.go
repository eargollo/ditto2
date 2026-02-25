package handlers

import (
	"database/sql"
	"log/slog"
	"net/http"
	"time"
)

// StatusHandler handles GET /api/status.
type StatusHandler struct {
	DB *sql.DB
}

type statusResponse struct {
	ActiveScan        *activeScanInfo    `json:"active_scan"`
	Schedule          scheduleInfo       `json:"schedule"`
	LastCompletedScan *completedScanInfo `json:"last_completed_scan"`
}

type activeScanInfo struct {
	ID          int64            `json:"id"`
	StartedAt   time.Time        `json:"started_at"`
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
	Cron      string     `json:"cron"`
	Paused    bool       `json:"paused"`
	NextRunAt *time.Time `json:"next_run_at"`
}

type completedScanInfo struct {
	ID               int64     `json:"id"`
	FinishedAt       time.Time `json:"finished_at"`
	DuplicateGroups  int64     `json:"duplicate_groups"`
	DuplicateFiles   int64     `json:"duplicate_files"`
	ReclaimableBytes int64     `json:"reclaimable_bytes"`
	CacheHits        int64     `json:"cache_hits"`
	CacheMisses      int64     `json:"cache_misses"`
	CacheHitRate     float64   `json:"cache_hit_rate"`
}

// ServeHTTP returns the system status as JSON.
func (h *StatusHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	resp := statusResponse{
		ActiveScan: h.activeScan(),
		Schedule: scheduleInfo{
			Cron:      "0 2 * * 0",
			Paused:    false,
			NextRunAt: nil,
		},
		LastCompletedScan: h.lastCompletedScan(),
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *StatusHandler) activeScan() *activeScanInfo {
	if h.DB == nil {
		return nil
	}
	row := h.DB.QueryRow(`
		SELECT id, started_at, triggered_by,
		       files_discovered,
		       progress_candidates_found, progress_partial_hashed,
		       progress_full_hashed, progress_bytes_read,
		       cache_hits, cache_misses
		FROM scan_history
		WHERE status = 'running'
		LIMIT 1`)

	var (
		id          int64
		startedAt   int64
		triggeredBy string
		prog        scanProgressInfo
	)
	err := row.Scan(&id, &startedAt, &triggeredBy,
		&prog.FilesDiscovered,
		&prog.CandidatesFound, &prog.PartialHashed,
		&prog.FullHashed, &prog.BytesRead,
		&prog.CacheHits, &prog.CacheMisses)
	if err != nil {
		if err != sql.ErrNoRows {
			slog.Error("status: query active scan", "error", err)
		}
		return nil
	}
	t := time.Unix(startedAt, 0).UTC()
	return &activeScanInfo{
		ID:          id,
		StartedAt:   t,
		TriggeredBy: triggeredBy,
		Progress:    prog,
	}
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
	total := cacheHits + cacheMisses
	if total > 0 {
		hitRate = float64(cacheHits) / float64(total)
	}
	return &completedScanInfo{
		ID:               id,
		FinishedAt:       time.Unix(finishedAt, 0).UTC(),
		DuplicateGroups:  duplicateGroups,
		DuplicateFiles:   duplicateFiles,
		ReclaimableBytes: reclaimableBytes,
		CacheHits:        cacheHits,
		CacheMisses:      cacheMisses,
		CacheHitRate:     hitRate,
	}
}
