package handlers

import (
	"database/sql"
	"net/http"
)

// GroupsHandler handles duplicate-group API endpoints.
type GroupsHandler struct {
	DB *sql.DB
}

// List handles GET /api/groups.
func (h *GroupsHandler) List(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, ListResponse[interface{}]{
		Items:  []interface{}{},
		Total:  0,
		Limit:  50,
		Offset: 0,
	})
}

// Get handles GET /api/groups/:id.
func (h *GroupsHandler) Get(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "not yet implemented")
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
