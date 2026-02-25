package handlers

import (
	"database/sql"
	"net/http"

	"github.com/eargollo/ditto/internal/config"
)

// ConfigHandler handles GET/PATCH /api/config.
type ConfigHandler struct {
	DB  *sql.DB
	Cfg *config.Config
}

// Get handles GET /api/config.
func (h *ConfigHandler) Get(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.Cfg)
}

// Update handles PATCH /api/config.
func (h *ConfigHandler) Update(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "config update not yet implemented")
}
