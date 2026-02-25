package handlers

import (
	"database/sql"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

// GroupsHandler handles duplicate-group API endpoints.
type GroupsHandler struct {
	DB *sql.DB
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
func (h *GroupsHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	status := q.Get("status")
	if status == "" {
		status = "unresolved"
	}
	fileType := q.Get("type")
	limit, offset := parsePagination(r)

	// Build WHERE clause.
	args := []interface{}{}
	where := ""
	if status != "all" {
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
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Group not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	g.CreatedAt = time.Unix(createdAt, 0).UTC().Format(time.RFC3339)
	g.UpdatedAt = time.Unix(updatedAt, 0).UTC().Format(time.RFC3339)

	// Fetch files in this group.
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
func (h *GroupsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "not yet implemented")
}

// Ignore handles POST /api/groups/:id/ignore.
func (h *GroupsHandler) Ignore(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "not yet implemented")
}

// Thumbnail handles GET /api/groups/:id/thumbnail.
func (h *GroupsHandler) Thumbnail(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, "NOT_FOUND", "no previewable file in group")
}
