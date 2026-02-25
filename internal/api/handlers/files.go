package handlers

import (
	"database/sql"
	"net/http"
)

// FilesHandler handles file-level API endpoints.
type FilesHandler struct {
	DB *sql.DB
}

// Thumbnail handles GET /api/files/:id/thumbnail.
func (h *FilesHandler) Thumbnail(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, "NOT_FOUND", "file not found or not previewable")
}

// Preview handles GET /api/files/:id/preview.
func (h *FilesHandler) Preview(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, "NOT_FOUND", "file not found or not previewable")
}
