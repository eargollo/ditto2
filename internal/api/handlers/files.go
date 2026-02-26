package handlers

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/eargollo/ditto/internal/media"
)

// FilesHandler handles file-level API endpoints.
type FilesHandler struct {
	DB *sql.DB
}

// fileInfoResponse is returned by GET /api/files/{id}/info.
type fileInfoResponse struct {
	ID       int64      `json:"id"`
	Path     string     `json:"path"`
	Filename string     `json:"filename"`
	Size     int64      `json:"size"`
	Modified time.Time  `json:"modified"`
	MimeType string     `json:"mime_type"`
	FileType string     `json:"file_type"`
	Image    *media.ImageMeta `json:"image,omitempty"`
}

// Info handles GET /api/files/{id}/info.
func (h *FilesHandler) Info(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "Invalid file ID")
		return
	}

	var path, fileType string
	var size int64
	var mtime int64
	err = h.DB.QueryRowContext(r.Context(),
		`SELECT path, file_type, size, mtime FROM duplicate_files WHERE id = ?`, id,
	).Scan(&path, &fileType, &size, &mtime)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "file not found")
		return
	}
	if err != nil {
		slog.Error("files info: db query", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	resp := fileInfoResponse{
		ID:       id,
		Path:     path,
		Filename: filepath.Base(path),
		Size:     size,
		Modified: time.Unix(mtime, 0).UTC(),
		MimeType: media.ContentType(path),
		FileType: fileType,
	}

	if fileType == string(media.FileTypeImage) {
		meta := media.ExtractImageMeta(path)
		resp.Image = &meta
	}

	writeJSON(w, http.StatusOK, resp)
}

// Thumbnail handles GET /api/files/:id/thumbnail.
// Returns a 320x320 JPEG thumbnail for image files.
func (h *FilesHandler) Thumbnail(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "Invalid file ID")
		return
	}

	var path string
	var fileType string
	err = h.DB.QueryRowContext(r.Context(),
		`SELECT path, file_type FROM duplicate_files WHERE id = ?`, id,
	).Scan(&path, &fileType)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "file not found or not previewable")
		return
	}
	if err != nil {
		slog.Error("files thumbnail: db query", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	if fileType != string(media.FileTypeImage) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "file not found or not previewable")
		return
	}

	thumb, err := media.Thumbnail(path, 320, 320)
	if err != nil {
		slog.Error("files thumbnail: generate", "id", id, "path", path, "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "thumbnail generation failed")
		return
	}
	if thumb == nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "file not found or not previewable")
		return
	}

	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.WriteHeader(http.StatusOK)
	w.Write(thumb) //nolint:errcheck
}

// Preview handles GET /api/files/:id/preview.
// Serves the original file with the correct Content-Type for lightbox use.
func (h *FilesHandler) Preview(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "Invalid file ID")
		return
	}

	var path string
	err = h.DB.QueryRowContext(r.Context(),
		`SELECT path FROM duplicate_files WHERE id = ?`, id,
	).Scan(&path)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "file not found or not previewable")
		return
	}
	if err != nil {
		slog.Error("files preview: db query", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	// Verify file still exists on disk.
	if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "file not found or not previewable")
		return
	}

	ct := media.ContentType(path)
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "public, max-age=3600")
	http.ServeFile(w, r, path)
}
