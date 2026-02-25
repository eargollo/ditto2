package handlers

import (
	"database/sql"
	"net/http"
)

// StatsHandler handles GET /api/stats.
type StatsHandler struct {
	DB *sql.DB
}

type statsResponse struct {
	Snapshots []interface{} `json:"snapshots"`
	Totals    statsTotals   `json:"totals"`
}

type statsTotals struct {
	DeletedFiles        int64 `json:"deleted_files"`
	ReclaimedBytes      int64 `json:"reclaimed_bytes"`
	DeletedFiles30d     int64 `json:"deleted_files_30d"`
	ReclaimedBytes30d   int64 `json:"reclaimed_bytes_30d"`
}

// ServeHTTP handles GET /api/stats.
func (h *StatsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, statsResponse{
		Snapshots: []interface{}{},
		Totals:    statsTotals{},
	})
}
