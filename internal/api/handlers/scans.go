package handlers

import (
	"database/sql"
	"net/http"
)

// ScansHandler handles scan-related API endpoints.
type ScansHandler struct {
	DB *sql.DB
}

// List handles GET /api/scans.
func (h *ScansHandler) List(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, ListResponse[interface{}]{
		Items:  []interface{}{},
		Total:  0,
		Limit:  50,
		Offset: 0,
	})
}

// Create handles POST /api/scans.
func (h *ScansHandler) Create(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "scan pipeline not yet implemented")
}

// Get handles GET /api/scans/:id.
func (h *ScansHandler) Get(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "not yet implemented")
}

// Cancel handles DELETE /api/scans/current.
func (h *ScansHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, "NO_ACTIVE_SCAN", "No scan is currently running")
}
