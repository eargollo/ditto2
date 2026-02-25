package handlers

import (
	"database/sql"
	"net/http"
)

// TrashHandler handles trash API endpoints.
type TrashHandler struct {
	DB *sql.DB
}

// List handles GET /api/trash.
func (h *TrashHandler) List(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, ListResponse[interface{}]{
		Items:  []interface{}{},
		Total:  0,
		Limit:  50,
		Offset: 0,
	})
}

// Restore handles POST /api/trash/:id/restore.
func (h *TrashHandler) Restore(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "not yet implemented")
}

// PurgeAll handles DELETE /api/trash.
func (h *TrashHandler) PurgeAll(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "not yet implemented")
}
