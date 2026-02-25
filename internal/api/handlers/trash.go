package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/eargollo/ditto/internal/trash"
)

// TrashHandler handles trash API endpoints.
type TrashHandler struct {
	DB    *sql.DB
	Trash *trash.Manager
}

// List handles GET /api/trash — active trash items sorted by trashed_at DESC.
func (h *TrashHandler) List(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePagination(r)

	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT id, original_path, file_size, content_hash, trashed_at, expires_at, group_id
		FROM trash
		WHERE status = 'trashed'
		ORDER BY trashed_at DESC
		LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		slog.Error("trash list: query", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	defer rows.Close()

	type trashItem struct {
		ID            int64  `json:"id"`
		OriginalPath  string `json:"original_path"`
		FileSize      int64  `json:"file_size"`
		ContentHash   string `json:"content_hash"`
		TrashedAt     string `json:"trashed_at"`
		ExpiresAt     string `json:"expires_at"`
		DaysRemaining int    `json:"days_remaining"`
		GroupID       *int64 `json:"group_id"`
	}

	var items []trashItem
	for rows.Next() {
		var it trashItem
		var trashedAt, expiresAt int64
		var groupID sql.NullInt64
		if err := rows.Scan(&it.ID, &it.OriginalPath, &it.FileSize, &it.ContentHash,
			&trashedAt, &expiresAt, &groupID); err != nil {
			slog.Error("trash list: scan row", "error", err)
			continue
		}
		it.TrashedAt = time.Unix(trashedAt, 0).UTC().Format(time.RFC3339)
		it.ExpiresAt = time.Unix(expiresAt, 0).UTC().Format(time.RFC3339)
		it.DaysRemaining = int(time.Until(time.Unix(expiresAt, 0)).Hours() / 24)
		if it.DaysRemaining < 0 {
			it.DaysRemaining = 0
		}
		if groupID.Valid {
			it.GroupID = &groupID.Int64
		}
		items = append(items, it)
	}
	if items == nil {
		items = []trashItem{}
	}

	var total int
	var totalSize int64
	h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*), COALESCE(SUM(file_size),0) FROM trash WHERE status='trashed'`,
	).Scan(&total, &totalSize)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"items":      items,
		"total":      total,
		"total_size": totalSize,
		"limit":      limit,
		"offset":     offset,
	})
}

// Restore handles POST /api/trash/:id/restore.
func (h *TrashHandler) Restore(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "Invalid trash ID")
		return
	}

	// Read original_path before restoring (for the response).
	var originalPath string
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT original_path FROM trash WHERE id = ? AND status = 'trashed'`, id,
	).Scan(&originalPath); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "Trash item not found or already purged/restored")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	if err := h.Trash.Restore(r.Context(), id); err != nil {
		if errors.Is(err, trash.ErrNotTrashed) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "Trash item not found or already purged/restored")
			return
		}
		var conflict *trash.ErrRestoreConflict
		if errors.As(err, &conflict) {
			writeError(w, http.StatusConflict, "RESTORE_PATH_CONFLICT",
				"A file already exists at the original path")
			return
		}
		slog.Error("trash restore", "trash_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":            id,
		"original_path": originalPath,
		"status":        "restored",
		"restored_at":   time.Now().UTC().Format(time.RFC3339),
	})
}

// PurgeAll handles DELETE /api/trash — requires {"confirm": true}.
func (h *TrashHandler) PurgeAll(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Confirm bool `json:"confirm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || !body.Confirm {
		writeError(w, http.StatusBadRequest, "CONFIRMATION_REQUIRED",
			"Set confirm: true to proceed with purge")
		return
	}

	count, bytesFreed, err := h.Trash.PurgeAll(r.Context())
	if err != nil {
		slog.Error("trash purge all", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"purged_count": count,
		"bytes_freed":  bytesFreed,
	})
}
