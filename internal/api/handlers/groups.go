package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/eargollo/ditto/internal/config"
	"github.com/eargollo/ditto/internal/scan"
	"github.com/eargollo/ditto/internal/trash"
)

// GroupsHandler handles duplicate-group API endpoints.
type GroupsHandler struct {
	DB      *sql.DB
	Trash   *trash.Manager
	Cfg     *config.Config
	ScanMgr *scan.Manager
	mu      sync.Mutex // guards Cfg mutations for dir-type ignore
}

type groupItem struct {
	ID               int64   `json:"id"`
	ContentHash      string  `json:"content_hash"`
	FileSize         int64   `json:"file_size"`
	FileCount        int     `json:"file_count"`
	ReclaimableBytes int64   `json:"reclaimable_bytes"`
	FileType         string  `json:"file_type"`
	Status           string  `json:"status"`
	ThumbnailURL     string  `json:"thumbnail_url"`
	CreatedAt        string  `json:"created_at"`
	UpdatedAt        string  `json:"updated_at"`
}

// List handles GET /api/groups.
// Default filter (no status param, or status=active) returns unresolved and watching_alert groups.
func (h *GroupsHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	status := q.Get("status")
	fileType := q.Get("type")
	limit, offset := parsePagination(r)

	args := []interface{}{}
	where := ""

	useActiveFilter := status == "" || status == "active"
	if useActiveFilter {
		where += " AND status IN ('unresolved','watching_alert')"
	} else if status != "all" {
		where += " AND status = ?"
		args = append(args, status)
	}
	if fileType != "" {
		where += " AND file_type = ?"
		args = append(args, fileType)
	}
	if minR := q.Get("min_reclaimable"); minR != "" {
		if v, err := strconv.ParseInt(minR, 10, 64); err == nil {
			where += " AND reclaimable_bytes >= ?"
			args = append(args, v)
		}
	}

	countArgs := append([]interface{}{}, args...)
	var total int
	h.DB.QueryRowContext(r.Context(),
		"SELECT COUNT(*) FROM duplicate_groups WHERE 1=1"+where,
		countArgs...,
	).Scan(&total)

	queryArgs := append(args, limit, offset)
	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT id, content_hash, file_size, file_count, reclaimable_bytes,
		       file_type, status, created_at, updated_at
		FROM duplicate_groups
		WHERE 1=1`+where+`
		ORDER BY reclaimable_bytes DESC
		LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		slog.Error("groups list: query", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	defer rows.Close()

	items := []groupItem{}
	for rows.Next() {
		var g groupItem
		var createdAt, updatedAt int64
		if err := rows.Scan(
			&g.ID, &g.ContentHash, &g.FileSize, &g.FileCount,
			&g.ReclaimableBytes, &g.FileType, &g.Status,
			&createdAt, &updatedAt,
		); err != nil {
			slog.Error("groups list: scan row", "error", err)
			continue
		}
		g.CreatedAt = time.Unix(createdAt, 0).UTC().Format(time.RFC3339)
		g.UpdatedAt = time.Unix(updatedAt, 0).UTC().Format(time.RFC3339)
		g.ThumbnailURL = "/api/groups/" + strconv.FormatInt(g.ID, 10) + "/thumbnail"
		items = append(items, g)
	}

	writeJSON(w, http.StatusOK, ListResponse[groupItem]{
		Items:  items,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	})
}

// Get handles GET /api/groups/:id.
func (h *GroupsHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "Invalid group ID")
		return
	}

	var g groupItem
	var createdAt, updatedAt int64
	err = h.DB.QueryRowContext(r.Context(), `
		SELECT id, content_hash, file_size, file_count, reclaimable_bytes,
		       file_type, status, created_at, updated_at
		FROM duplicate_groups WHERE id = ?`, id,
	).Scan(
		&g.ID, &g.ContentHash, &g.FileSize, &g.FileCount,
		&g.ReclaimableBytes, &g.FileType, &g.Status,
		&createdAt, &updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Group not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	g.CreatedAt = time.Unix(createdAt, 0).UTC().Format(time.RFC3339)
	g.UpdatedAt = time.Unix(updatedAt, 0).UTC().Format(time.RFC3339)

	type fileItem struct {
		ID           int64  `json:"id"`
		Path         string `json:"path"`
		Size         int64  `json:"size"`
		MTime        string `json:"mtime"`
		FileType     string `json:"file_type"`
		ThumbnailURL string `json:"thumbnail_url"`
		PreviewURL   string `json:"preview_url"`
	}
	fileRows, err := h.DB.QueryContext(r.Context(), `
		SELECT id, path, size, mtime, file_type
		FROM duplicate_files WHERE group_id = ?
		ORDER BY path`, id)
	if err != nil {
		slog.Error("groups get: query files", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	defer fileRows.Close()

	var files []fileItem
	for fileRows.Next() {
		var f fileItem
		var mtime int64
		if err := fileRows.Scan(&f.ID, &f.Path, &f.Size, &mtime, &f.FileType); err != nil {
			continue
		}
		f.MTime = time.Unix(mtime, 0).UTC().Format(time.RFC3339)
		fid := strconv.FormatInt(f.ID, 10)
		f.ThumbnailURL = "/api/files/" + fid + "/thumbnail"
		f.PreviewURL = "/api/files/" + fid + "/preview"
		files = append(files, f)
	}
	if files == nil {
		files = []fileItem{}
	}

	type groupDetail struct {
		groupItem
		Files []fileItem `json:"files"`
	}
	writeJSON(w, http.StatusOK, groupDetail{groupItem: g, Files: files})
}

// Delete handles POST /api/groups/:id/delete.
// Validates files have not changed since last scan, then moves selected files to trash.
func (h *GroupsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	groupID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "Invalid group ID")
		return
	}

	var body struct {
		DeleteFileIDs []int64 `json:"delete_file_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.DeleteFileIDs) == 0 {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "delete_file_ids is required and must be non-empty")
		return
	}

	// Load group metadata.
	var contentHash string
	var fileSize int64
	err = h.DB.QueryRowContext(r.Context(),
		`SELECT content_hash, file_size FROM duplicate_groups WHERE id = ?`, groupID,
	).Scan(&contentHash, &fileSize)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Group not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	// Load all files in the group.
	type fileRecord struct {
		ID    int64
		Path  string
		Size  int64
		MTime int64
	}
	fileRows, err := h.DB.QueryContext(r.Context(),
		`SELECT id, path, size, mtime FROM duplicate_files WHERE group_id = ?`, groupID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	defer fileRows.Close()

	allFiles := map[int64]fileRecord{}
	for fileRows.Next() {
		var f fileRecord
		if err := fileRows.Scan(&f.ID, &f.Path, &f.Size, &f.MTime); err != nil {
			continue
		}
		allFiles[f.ID] = f
	}
	fileRows.Close()

	// Build delete set and validate at least one keeper.
	deleteSet := make(map[int64]bool, len(body.DeleteFileIDs))
	for _, id := range body.DeleteFileIDs {
		deleteSet[id] = true
	}
	keepCount := len(allFiles) - len(deleteSet)
	if keepCount < 1 {
		writeError(w, http.StatusBadRequest, "NO_KEEPER", "At least one file must be kept in the group")
		return
	}

	// Pre-deletion validation: stat every file.
	type validationFailure struct {
		FileID int64  `json:"file_id"`
		Path   string `json:"path"`
		Reason string `json:"reason"`
	}
	var failures []validationFailure

	for id, f := range allFiles {
		info, statErr := os.Stat(f.Path)
		if deleteSet[id] {
			if os.IsNotExist(statErr) {
				failures = append(failures, validationFailure{id, f.Path, "FILE_MISSING"})
			} else if statErr == nil && (info.Size() != f.Size || info.ModTime().Unix() != f.MTime) {
				failures = append(failures, validationFailure{id, f.Path, "FILE_MODIFIED"})
			}
		} else {
			if os.IsNotExist(statErr) {
				failures = append(failures, validationFailure{id, f.Path, "KEEPER_MISSING"})
			} else if statErr == nil && (info.Size() != f.Size || info.ModTime().Unix() != f.MTime) {
				failures = append(failures, validationFailure{id, f.Path, "KEEPER_MODIFIED"})
			}
		}
	}

	if len(failures) > 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"code":     "VALIDATION_FAILED",
				"message":  "One or more files have changed since the last scan. Please re-scan.",
				"failures": failures,
			},
		})
		return
	}

	// Move files to trash (outside any DB transaction — MoveToTrash has its own DB writes).
	retentionDays := 30
	if h.Cfg != nil && h.Cfg.TrashRetentionDays > 0 {
		retentionDays = h.Cfg.TrashRetentionDays
	}

	type trashedItem struct {
		FileID       int64  `json:"file_id"`
		TrashID      int64  `json:"trash_id"`
		OriginalPath string `json:"original_path"`
		ExpiresAt    string `json:"expires_at"`
	}

	var trashed []trashedItem
	expiresAt := time.Now().Add(time.Duration(retentionDays) * 24 * time.Hour).UTC()

	for _, fileID := range body.DeleteFileIDs {
		f := allFiles[fileID]
		trashID, err := h.Trash.MoveToTrash(r.Context(), f.Path, groupID, contentHash, retentionDays)
		if err != nil {
			slog.Error("group delete: move to trash", "file_id", fileID, "path", f.Path, "error", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to move file to trash: "+err.Error())
			return
		}
		trashed = append(trashed, trashedItem{
			FileID:       fileID,
			TrashID:      trashID,
			OriginalPath: f.Path,
			ExpiresAt:    expiresAt.Format(time.RFC3339),
		})
	}

	// Remove trashed files from duplicate_files and update group stats.
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	defer tx.Rollback()

	for _, fileID := range body.DeleteFileIDs {
		if _, err := tx.ExecContext(r.Context(),
			`DELETE FROM duplicate_files WHERE id = ?`, fileID); err != nil {
			slog.Error("group delete: remove duplicate_file", "file_id", fileID, "error", err)
		}
	}

	remainingCount := len(allFiles) - len(body.DeleteFileIDs)
	newStatus := "unresolved"
	newReclaimable := fileSize * int64(remainingCount-1)
	if remainingCount <= 1 {
		newStatus = "resolved"
		newReclaimable = 0
	}

	now := time.Now().Unix()
	resolvedAt := "NULL"
	_ = resolvedAt
	if newStatus == "resolved" {
		if _, err := tx.ExecContext(r.Context(), `
			UPDATE duplicate_groups
			SET file_count=?, reclaimable_bytes=?, status=?, resolved_at=?, updated_at=?
			WHERE id=?`,
			remainingCount, newReclaimable, newStatus, now, now, groupID); err != nil {
			slog.Error("group delete: update group (resolved)", "error", err)
		}
	} else {
		if _, err := tx.ExecContext(r.Context(), `
			UPDATE duplicate_groups
			SET file_count=?, reclaimable_bytes=?, status=?, updated_at=?
			WHERE id=?`,
			remainingCount, newReclaimable, newStatus, now, groupID); err != nil {
			slog.Error("group delete: update group", "error", err)
		}
	}

	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"trashed": trashed,
		"group": map[string]interface{}{
			"id":                groupID,
			"file_count":        remainingCount,
			"reclaimable_bytes": newReclaimable,
			"status":            newStatus,
		},
	})
}

// Ignore handles POST /api/groups/:id/ignore.
// type=hash: suppress this content hash forever.
// type=path_pair: watch this set of paths; alert if they diverge or a new copy appears.
// type=dir: exclude a directory from future scans.
func (h *GroupsHandler) Ignore(w http.ResponseWriter, r *http.Request) {
	groupID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "Invalid group ID")
		return
	}

	var body struct {
		Type string `json:"type"`
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid request body")
		return
	}

	// Load group.
	var contentHash string
	err = h.DB.QueryRowContext(r.Context(),
		`SELECT content_hash FROM duplicate_groups WHERE id = ?`, groupID,
	).Scan(&contentHash)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Group not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	now := time.Now().Unix()
	var whitelistID int64
	var whitelistValue string
	var newGroupStatus string

	switch body.Type {
	case "hash":
		whitelistValue = contentHash
		newGroupStatus = "ignored"

		res, err := h.DB.ExecContext(r.Context(),
			`INSERT OR IGNORE INTO whitelist (type, value, added_by, added_at)
			 VALUES ('hash', ?, 'user', ?)`,
			whitelistValue, now)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		whitelistID, _ = res.LastInsertId()
		if whitelistID == 0 {
			h.DB.QueryRowContext(r.Context(),
				`SELECT id FROM whitelist WHERE type='hash' AND value=?`, whitelistValue,
			).Scan(&whitelistID)
		}

	case "path_pair":
		// Load all paths sorted for a stable canonical value.
		pathRows, err := h.DB.QueryContext(r.Context(),
			`SELECT path FROM duplicate_files WHERE group_id = ? ORDER BY path`, groupID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		var paths []string
		for pathRows.Next() {
			var p string
			if err := pathRows.Scan(&p); err == nil {
				paths = append(paths, p)
			}
		}
		pathRows.Close()

		if len(paths) < 2 {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Group must have at least 2 files for a path_pair watch")
			return
		}
		sort.Strings(paths)
		pathJSON, _ := json.Marshal(paths)
		whitelistValue = string(pathJSON)
		newGroupStatus = "watching"

		res, err := h.DB.ExecContext(r.Context(),
			`INSERT OR IGNORE INTO whitelist (type, value, expected_hash, added_by, added_at)
			 VALUES ('path_pair', ?, ?, 'user', ?)`,
			whitelistValue, contentHash, now)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		whitelistID, _ = res.LastInsertId()
		if whitelistID == 0 {
			h.DB.QueryRowContext(r.Context(),
				`SELECT id FROM whitelist WHERE type='path_pair' AND value=?`, whitelistValue,
			).Scan(&whitelistID)
		}

	case "dir":
		if body.Path == "" {
			writeError(w, http.StatusBadRequest, "MISSING_PATH", "path is required for type=dir")
			return
		}
		whitelistValue = body.Path
		newGroupStatus = "ignored"

		res, err := h.DB.ExecContext(r.Context(),
			`INSERT OR IGNORE INTO whitelist (type, value, added_by, added_at)
			 VALUES ('dir', ?, 'user', ?)`,
			whitelistValue, now)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		whitelistID, _ = res.LastInsertId()
		if whitelistID == 0 {
			h.DB.QueryRowContext(r.Context(),
				`SELECT id FROM whitelist WHERE type='dir' AND value=?`, whitelistValue,
			).Scan(&whitelistID)
		}

		// Update in-memory config so the exclusion takes effect on the next scan.
		if h.Cfg != nil && h.ScanMgr != nil {
			h.mu.Lock()
			h.Cfg.ExcludePaths = append(h.Cfg.ExcludePaths, body.Path)
			excludes := append([]string{}, h.Cfg.ExcludePaths...)
			scanPaths := append([]string{}, h.Cfg.ScanPaths...)
			scanCfg := scan.Config{
				Walkers:        h.Cfg.ScanWorkers.Walkers,
				PartialHashers: h.Cfg.ScanWorkers.PartialHashers,
				FullHashers:    h.Cfg.ScanWorkers.FullHashers,
				BatchSize:      1000,
			}
			h.mu.Unlock()
			h.ScanMgr.UpdateConfig(scanPaths, excludes, scanCfg)
		}

	default:
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "type must be 'hash', 'path_pair', or 'dir'")
		return
	}

	// Update group status.
	if _, err := h.DB.ExecContext(r.Context(), `
		UPDATE duplicate_groups
		SET status=?, ignored_at=?, updated_at=?
		WHERE id=?`,
		newGroupStatus, now, now, groupID); err != nil {
		slog.Error("group ignore: update status", "group_id", groupID, "error", err)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"whitelist_id": whitelistID,
		"type":         body.Type,
		"value":        whitelistValue,
		"group": map[string]interface{}{
			"id":     groupID,
			"status": newGroupStatus,
		},
	})
}

// Thumbnail handles GET /api/groups/:id/thumbnail.
func (h *GroupsHandler) Thumbnail(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, "NOT_FOUND", "no previewable file in group")
}
