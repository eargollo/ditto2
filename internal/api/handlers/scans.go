package handlers

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/eargollo/ditto/internal/scan"
)

// ScansHandler handles scan-related API endpoints.
type ScansHandler struct {
	DB      *sql.DB
	Manager *scan.Manager
}

// Create handles POST /api/scans — triggers a manual scan.
func (h *ScansHandler) Create(w http.ResponseWriter, r *http.Request) {
	active, err := h.Manager.Start(context.Background(), "manual")
	if err != nil {
		if errors.Is(err, scan.ErrAlreadyRunning) {
			writeError(w, http.StatusConflict, "SCAN_ALREADY_RUNNING", "A scan is already in progress")
			return
		}
		slog.Error("scans: start", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to start scan")
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"id":           active.ID, // may be 0 momentarily until goroutine sets it
		"status":       "running",
		"started_at":   active.StartedAt.UTC().Format(time.RFC3339),
		"triggered_by": active.TriggeredBy,
	})
}

// Cancel handles DELETE /api/scans/current.
func (h *ScansHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	snap, err := h.Manager.Cancel()
	if err != nil {
		if errors.Is(err, scan.ErrNoActiveScan) {
			writeError(w, http.StatusNotFound, "NO_ACTIVE_SCAN", "No scan is currently running")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":           snap.ID,
		"status":       "cancelled",
		"started_at":   snap.StartedAt.UTC().Format(time.RFC3339),
		"finished_at":  time.Now().UTC().Format(time.RFC3339),
	})
}

// List handles GET /api/scans — returns scan history newest first.
func (h *ScansHandler) List(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePagination(r)

	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT id, started_at, finished_at, status, triggered_by,
		       files_discovered, files_hashed, cache_hits, cache_misses,
		       duplicate_groups, duplicate_files, reclaimable_bytes,
		       errors, duration_seconds
		FROM scan_history
		ORDER BY started_at DESC
		LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		slog.Error("scans list: query", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	defer rows.Close()

	type scanItem struct {
		ID               int64    `json:"id"`
		StartedAt        string   `json:"started_at"`
		FinishedAt       *string  `json:"finished_at"`
		Status           string   `json:"status"`
		TriggeredBy      string   `json:"triggered_by"`
		FilesDiscovered  int64    `json:"files_discovered"`
		FilesHashed      int64    `json:"files_hashed"`
		CacheHits        int64    `json:"cache_hits"`
		CacheMisses      int64    `json:"cache_misses"`
		CacheHitRate     float64  `json:"cache_hit_rate"`
		DuplicateGroups  int64    `json:"duplicate_groups"`
		DuplicateFiles   int64    `json:"duplicate_files"`
		ReclaimableBytes int64    `json:"reclaimable_bytes"`
		Errors           int64    `json:"errors"`
		DurationSeconds  *int64   `json:"duration_seconds"`
	}

	var items []scanItem
	for rows.Next() {
		var it scanItem
		var startedAt int64
		var finishedAt sql.NullInt64
		var durSecs sql.NullInt64
		if err := rows.Scan(
			&it.ID, &startedAt, &finishedAt, &it.Status, &it.TriggeredBy,
			&it.FilesDiscovered, &it.FilesHashed, &it.CacheHits, &it.CacheMisses,
			&it.DuplicateGroups, &it.DuplicateFiles, &it.ReclaimableBytes,
			&it.Errors, &durSecs,
		); err != nil {
			slog.Error("scans list: scan row", "error", err)
			continue
		}
		it.StartedAt = time.Unix(startedAt, 0).UTC().Format(time.RFC3339)
		if finishedAt.Valid {
			s := time.Unix(finishedAt.Int64, 0).UTC().Format(time.RFC3339)
			it.FinishedAt = &s
		}
		if durSecs.Valid {
			it.DurationSeconds = &durSecs.Int64
		}
		total := it.CacheHits + it.CacheMisses
		if total > 0 {
			it.CacheHitRate = float64(it.CacheHits) / float64(total)
		}
		items = append(items, it)
	}
	if items == nil {
		items = []scanItem{}
	}

	var total int
	h.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM scan_history`).Scan(&total)

	writeJSON(w, http.StatusOK, ListResponse[scanItem]{
		Items:  items,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	})
}

// Get handles GET /api/scans/:id.
func (h *ScansHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "Invalid scan ID")
		return
	}

	type errItem struct {
		Path       string `json:"path"`
		Stage      string `json:"stage"`
		Error      string `json:"error"`
		OccurredAt string `json:"occurred_at"`
	}
	type scanDetail struct {
		ID               int64     `json:"id"`
		StartedAt        string    `json:"started_at"`
		FinishedAt       *string   `json:"finished_at"`
		Status           string    `json:"status"`
		TriggeredBy      string    `json:"triggered_by"`
		FilesDiscovered  int64     `json:"files_discovered"`
		FilesHashed      int64     `json:"files_hashed"`
		CacheHits        int64     `json:"cache_hits"`
		CacheMisses      int64     `json:"cache_misses"`
		CacheHitRate     float64   `json:"cache_hit_rate"`
		DuplicateGroups  int64     `json:"duplicate_groups"`
		DuplicateFiles   int64     `json:"duplicate_files"`
		ReclaimableBytes int64     `json:"reclaimable_bytes"`
		Errors           int64     `json:"errors"`
		DurationSeconds  *int64    `json:"duration_seconds"`
		ErrorList        []errItem `json:"error_list"`
	}

	var d scanDetail
	var startedAt int64
	var finishedAt sql.NullInt64
	var durSecs sql.NullInt64
	err = h.DB.QueryRowContext(r.Context(), `
		SELECT id, started_at, finished_at, status, triggered_by,
		       files_discovered, files_hashed, cache_hits, cache_misses,
		       duplicate_groups, duplicate_files, reclaimable_bytes,
		       errors, duration_seconds
		FROM scan_history WHERE id = ?`, id,
	).Scan(
		&d.ID, &startedAt, &finishedAt, &d.Status, &d.TriggeredBy,
		&d.FilesDiscovered, &d.FilesHashed, &d.CacheHits, &d.CacheMisses,
		&d.DuplicateGroups, &d.DuplicateFiles, &d.ReclaimableBytes,
		&d.Errors, &durSecs,
	)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Scan not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	d.StartedAt = time.Unix(startedAt, 0).UTC().Format(time.RFC3339)
	if finishedAt.Valid {
		s := time.Unix(finishedAt.Int64, 0).UTC().Format(time.RFC3339)
		d.FinishedAt = &s
	}
	if durSecs.Valid {
		d.DurationSeconds = &durSecs.Int64
	}
	total := d.CacheHits + d.CacheMisses
	if total > 0 {
		d.CacheHitRate = float64(d.CacheHits) / float64(total)
	}

	// Fetch error list.
	errRows, _ := h.DB.QueryContext(r.Context(), `
		SELECT path, stage, error, occurred_at
		FROM scan_errors WHERE scan_id = ?
		ORDER BY occurred_at`, id)
	if errRows != nil {
		defer errRows.Close()
		for errRows.Next() {
			var e errItem
			var occAt int64
			if errRows.Scan(&e.Path, &e.Stage, &e.Error, &occAt) == nil {
				e.OccurredAt = time.Unix(occAt, 0).UTC().Format(time.RFC3339)
				d.ErrorList = append(d.ErrorList, e)
			}
		}
	}
	if d.ErrorList == nil {
		d.ErrorList = []errItem{}
	}

	writeJSON(w, http.StatusOK, d)
}

// Telemetry handles GET /api/scans/:id/telemetry — returns structured
// efficiency metrics for the scan suitable for benchmarking and analysis.
func (h *ScansHandler) Telemetry(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "Invalid scan ID")
		return
	}

	type telemetryResponse struct {
		ScanID          int64   `json:"scan_id"`
		StartedAt       string  `json:"started_at"`
		FinishedAt      *string `json:"finished_at"`
		Status          string  `json:"status"`
		TriggeredBy     string  `json:"triggered_by"`
		// Raw counters
		DurationSeconds  int64   `json:"duration_seconds"`
		FilesDiscovered  int64   `json:"files_discovered"`
		FilesHashed      int64   `json:"files_hashed"`
		CacheHits        int64   `json:"cache_hits"`
		CacheMisses      int64   `json:"cache_misses"`
		DuplicateGroups  int64   `json:"duplicate_groups"`
		DuplicateFiles   int64   `json:"duplicate_files"`
		ReclaimableBytes int64   `json:"reclaimable_bytes"`
		Errors           int64   `json:"errors"`
		BytesReadMB      float64 `json:"bytes_read_mb"`
		DiskReadMs       int64   `json:"disk_read_ms"`
		DBReadMs         int64   `json:"db_read_ms"`
		DBWriteMs        int64   `json:"db_write_ms"`
		TotalTimingMs    int64   `json:"total_timing_ms"`
		// Computed efficiency metrics
		FilesPerSec        float64 `json:"files_per_sec"`
		CandidatePct       float64 `json:"candidate_pct"`
		CacheHitPct        float64 `json:"cache_hit_pct"`
		HashThroughputMBps float64 `json:"hash_throughput_mbps"`
		DiskPct            float64 `json:"disk_pct"`
		DBWritePct         float64 `json:"db_write_pct"`
		DBReadPct          float64 `json:"db_read_pct"`
	}

	var d telemetryResponse
	var startedAt int64
	var finishedAt sql.NullInt64
	var durSecs sql.NullInt64
	var bytesRead int64
	err = h.DB.QueryRowContext(r.Context(), `
		SELECT id, started_at, finished_at, status, triggered_by,
		       files_discovered, files_hashed, cache_hits, cache_misses,
		       duplicate_groups, duplicate_files, reclaimable_bytes,
		       errors, duration_seconds,
		       progress_bytes_read, disk_read_ms, db_read_ms, db_write_ms
		FROM scan_history WHERE id = ?`, id,
	).Scan(
		&d.ScanID, &startedAt, &finishedAt, &d.Status, &d.TriggeredBy,
		&d.FilesDiscovered, &d.FilesHashed, &d.CacheHits, &d.CacheMisses,
		&d.DuplicateGroups, &d.DuplicateFiles, &d.ReclaimableBytes,
		&d.Errors, &durSecs,
		&bytesRead, &d.DiskReadMs, &d.DBReadMs, &d.DBWriteMs,
	)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Scan not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	d.StartedAt = time.Unix(startedAt, 0).UTC().Format(time.RFC3339)
	if finishedAt.Valid {
		s := time.Unix(finishedAt.Int64, 0).UTC().Format(time.RFC3339)
		d.FinishedAt = &s
	}
	if durSecs.Valid {
		d.DurationSeconds = durSecs.Int64
	}

	d.BytesReadMB = float64(bytesRead) / 1024 / 1024
	d.TotalTimingMs = d.DiskReadMs + d.DBReadMs + d.DBWriteMs

	// Derived efficiency metrics.
	if d.DurationSeconds > 0 {
		d.FilesPerSec = float64(d.FilesDiscovered) / float64(d.DurationSeconds)
	}
	totalCandidates := d.CacheHits + d.CacheMisses
	if d.FilesDiscovered > 0 {
		d.CandidatePct = float64(totalCandidates) * 100 / float64(d.FilesDiscovered)
	}
	if totalCandidates > 0 {
		d.CacheHitPct = float64(d.CacheHits) * 100 / float64(totalCandidates)
	}
	if d.DiskReadMs > 0 {
		d.HashThroughputMBps = float64(bytesRead) / (float64(d.DiskReadMs) / 1000.0) / 1024 / 1024
	}
	if d.TotalTimingMs > 0 {
		d.DiskPct = float64(d.DiskReadMs) * 100 / float64(d.TotalTimingMs)
		d.DBWritePct = float64(d.DBWriteMs) * 100 / float64(d.TotalTimingMs)
		d.DBReadPct = float64(d.DBReadMs) * 100 / float64(d.TotalTimingMs)
	}

	writeJSON(w, http.StatusOK, d)
}

// parsePagination extracts limit and offset from query parameters.
func parsePagination(r *http.Request) (limit, offset int) {
	limit = 50
	offset = 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	return
}
