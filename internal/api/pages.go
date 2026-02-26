package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/eargollo/ditto/internal/api/handlers"
	"github.com/eargollo/ditto/internal/config"
	"github.com/eargollo/ditto/internal/scan"
	"github.com/eargollo/ditto/internal/scheduler"
	"github.com/eargollo/ditto/internal/trash"
)

// ── Template helpers ──────────────────────────────────────────────────────────

var templateFuncs = template.FuncMap{
	"humanBytes": humanBytes,
	"add":        func(a, b int) int { return a + b },
	"sub":        func(a, b int) int { return a - b },
	"base":       filepath.Base,
	"truncate": func(s string, n int) string {
		if len(s) <= n {
			return s
		}
		return s[:n]
	},
	"json": func(v any) template.JS {
		b, _ := json.Marshal(v)
		return template.JS(b)
	},
}

func formatDuration(secs int64) string {
	if secs < 60 {
		return fmt.Sprintf("%ds", secs)
	}
	if secs < 3600 {
		return fmt.Sprintf("%dm %ds", secs/60, secs%60)
	}
	return fmt.Sprintf("%dh %dm", secs/3600, (secs%3600)/60)
}

func humanBytes(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	const unit = 1024
	div, exp := int64(unit), 0
	for rem := n / unit; rem >= unit; rem /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// ── Page data types ───────────────────────────────────────────────────────────

type baseData struct {
	FlashType    string
	FlashMessage string
}

type scanStatusData struct {
	ScanRunning     bool
	ScanID          int64
	StartedAt       string
	TriggeredBy     string
	FilesDiscovered int64
	CandidatesFound int64
	PartialHashed   int64
	FullHashed      int64
	BytesRead       int64
	HasLastScan     bool
	LastFinishedAt  string
	LastGroups      int64
	LastReclaimable int64
	CronExpr        string
	NextRunAt       string
}

type scanHistoryItem struct {
	ID               int64
	StartedAt        string
	Duration         string
	FilesDiscovered  int64
	DuplicateGroups  int64
	ReclaimableBytes int64
	ErrorCount       int64
	Status           string
	TriggeredBy      string
}

type snapshotPoint struct {
	Date             string
	DuplicateGroups  int64
	ReclaimableBytes int64
	CumReclaimed     int64
}

type dashboardData struct {
	baseData
	// Current active state
	Groups      int64
	Files       int64
	Reclaimable int64
	// Deletion history
	DeletedAllTime   int64
	ReclaimedAllTime int64
	Deleted30d       int64
	Reclaimed30d     int64
	// Recent scan history
	RecentScans []scanHistoryItem
	// Trend chart data (populated when len >= 3)
	Snapshots []snapshotPoint
}

type groupPageItem struct {
	ID               int64
	ContentHash      string
	HashShort        string
	DisplayName      string // basename of first file in the group
	FileSize         int64
	FileCount        int
	ReclaimableBytes int64
	FileType         string
	Status           string
}

// groupWithFiles is a groupPageItem with its files pre-loaded.
type groupWithFiles struct {
	groupPageItem
	Files []groupFileItem
}

type groupsPageData struct {
	baseData
	Groups           []groupWithFiles
	Total            int
	Limit            int
	Offset           int
	StatusFilter     string
	TypeFilter       string
	NextOffset       int
	PrevOffset       int
	HasNext          bool
	HasPrev          bool
	TotalActiveGroups int64
	TotalActiveFiles  int64
	TotalReclaimable  int64
}

type groupFileItem struct {
	ID       int64
	Path     string
	Size     int64
	MTime    string
	FileType string
}

type groupDetailData struct {
	baseData
	Group    groupPageItem
	Files    []groupFileItem
	NotFound bool
}

type trashPageItem struct {
	ID            int64
	OriginalPath  string
	FileSize      int64
	TrashedAt     string
	DaysRemaining int
	GroupID       *int64
}

type trashPageData struct {
	baseData
	Items      []trashPageItem
	Total      int
	TotalSize  int64
	Limit      int
	Offset     int
	NextOffset int
	PrevOffset int
	HasNext    bool
	HasPrev    bool
}

type settingsPageData struct {
	baseData
	ScanPaths          string
	ExcludePaths       string
	Schedule           string
	ScanPaused         bool
	TrashRetentionDays int
	Walkers            int
	PartialHashers     int
	FullHashers        int
}

// ── pageServer ────────────────────────────────────────────────────────────────

type pageServer struct {
	db          *sql.DB
	mgr         *scan.Manager
	trashMgr    *trash.Manager
	cfg         *config.Config
	sched       *scheduler.Scheduler
	templatesFS fs.FS
	cfgH        *handlers.ConfigHandler
}

func (ps *pageServer) renderTemplate(w http.ResponseWriter, pageName string, data any) {
	tmpl, err := template.New("").Funcs(templateFuncs).ParseFS(ps.templatesFS, "base.html", pageName)
	if err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "base", data); err != nil {
		slog.Error("template execute", "name", pageName, "error", err)
	}
}

func (ps *pageServer) renderFragment(w http.ResponseWriter, fileName, tmplName string, data any) {
	tmpl, err := template.New("").Funcs(templateFuncs).ParseFS(ps.templatesFS, fileName)
	if err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, tmplName, data); err != nil {
		slog.Error("fragment execute", "name", tmplName, "error", err)
	}
}

func flashFromQuery(r *http.Request) baseData {
	return baseData{
		FlashType:    r.URL.Query().Get("flash"),
		FlashMessage: r.URL.Query().Get("msg"),
	}
}

func uiRedirect(w http.ResponseWriter, r *http.Request, to, flashType, flashMsg string) {
	if flashMsg != "" {
		to += "?flash=" + url.QueryEscape(flashType) + "&msg=" + url.QueryEscape(flashMsg)
	}
	http.Redirect(w, r, to, http.StatusSeeOther)
}

// ── Page handlers ─────────────────────────────────────────────────────────────

func (ps *pageServer) dashboardPage(w http.ResponseWriter, r *http.Request) {
	d := dashboardData{baseData: flashFromQuery(r)}

	// Current active groups.
	ps.db.QueryRowContext(r.Context(), `
		SELECT COALESCE(SUM(1),0), COALESCE(SUM(file_count),0), COALESCE(SUM(reclaimable_bytes),0)
		FROM duplicate_groups WHERE status IN ('unresolved','watching_alert')
	`).Scan(&d.Groups, &d.Files, &d.Reclaimable)

	// All-time deletion stats.
	ps.db.QueryRowContext(r.Context(),
		`SELECT COUNT(*), COALESCE(SUM(file_size),0) FROM deletion_log`,
	).Scan(&d.DeletedAllTime, &d.ReclaimedAllTime)

	// Last-30-day deletion stats.
	since30d := time.Now().Add(-30 * 24 * time.Hour).Unix()
	ps.db.QueryRowContext(r.Context(),
		`SELECT COUNT(*), COALESCE(SUM(file_size),0) FROM deletion_log WHERE deleted_at >= ?`,
		since30d,
	).Scan(&d.Deleted30d, &d.Reclaimed30d)

	// Recent scan history (last 10 finished scans).
	scanRows, err := ps.db.QueryContext(r.Context(), `
		SELECT id, started_at, COALESCE(duration_seconds,0),
		       files_discovered, duplicate_groups, reclaimable_bytes,
		       errors, status, triggered_by
		FROM scan_history
		WHERE status IN ('completed','failed','cancelled')
		ORDER BY started_at DESC
		LIMIT 10`)
	if err == nil {
		defer scanRows.Close()
		for scanRows.Next() {
			var item scanHistoryItem
			var startedAt, durSecs int64
			if err := scanRows.Scan(&item.ID, &startedAt, &durSecs,
				&item.FilesDiscovered, &item.DuplicateGroups, &item.ReclaimableBytes,
				&item.ErrorCount, &item.Status, &item.TriggeredBy); err != nil {
				continue
			}
			item.StartedAt = time.Unix(startedAt, 0).Format("Jan 2, 2006 15:04")
			item.Duration = formatDuration(durSecs)
			d.RecentScans = append(d.RecentScans, item)
		}
	}
	if d.RecentScans == nil {
		d.RecentScans = []scanHistoryItem{}
	}

	// Trend chart snapshots (all, ascending).
	snapRows, err := ps.db.QueryContext(r.Context(), `
		SELECT snapshot_at, duplicate_groups, reclaimable_bytes, cumulative_reclaimed_bytes
		FROM scan_snapshots ORDER BY snapshot_at ASC`)
	if err == nil {
		defer snapRows.Close()
		for snapRows.Next() {
			var sp snapshotPoint
			var snapAt int64
			if err := snapRows.Scan(&snapAt, &sp.DuplicateGroups,
				&sp.ReclaimableBytes, &sp.CumReclaimed); err != nil {
				continue
			}
			sp.Date = time.Unix(snapAt, 0).Format("Jan 2")
			d.Snapshots = append(d.Snapshots, sp)
		}
	}
	if d.Snapshots == nil {
		d.Snapshots = []snapshotPoint{}
	}

	ps.renderTemplate(w, "dashboard.html", d)
}

func (ps *pageServer) groupsPage(w http.ResponseWriter, r *http.Request) {
	const pageLimit = 20
	q := r.URL.Query()
	statusFilter := q.Get("status")
	typeFilter := q.Get("type")
	offset := 0
	if v, err := strconv.Atoi(q.Get("offset")); err == nil && v >= 0 {
		offset = v
	}

	args := []interface{}{}
	where := ""
	useActive := statusFilter == "" || statusFilter == "active"
	if useActive {
		where += " AND status IN ('unresolved','watching_alert')"
		statusFilter = ""
	} else if statusFilter != "all" {
		where += " AND status = ?"
		args = append(args, statusFilter)
	}
	if typeFilter != "" {
		where += " AND file_type = ?"
		args = append(args, typeFilter)
	}

	// Overall active stats (always from active groups, regardless of filter).
	var totalActiveGroups, totalActiveFiles, totalReclaimable int64
	ps.db.QueryRowContext(r.Context(), `
		SELECT COALESCE(SUM(1),0), COALESCE(SUM(file_count),0), COALESCE(SUM(reclaimable_bytes),0)
		FROM duplicate_groups WHERE status IN ('unresolved','watching_alert')
	`).Scan(&totalActiveGroups, &totalActiveFiles, &totalReclaimable)

	var total int
	ps.db.QueryRowContext(r.Context(),
		"SELECT COUNT(*) FROM duplicate_groups WHERE 1=1"+where,
		args...,
	).Scan(&total)

	queryArgs := append(append([]interface{}{}, args...), pageLimit, offset)
	rows, err := ps.db.QueryContext(r.Context(), `
		SELECT id, content_hash, file_size, file_count, reclaimable_bytes, file_type, status
		FROM duplicate_groups
		WHERE 1=1`+where+`
		ORDER BY reclaimable_bytes DESC
		LIMIT ? OFFSET ?`, queryArgs...)

	var groups []groupWithFiles
	var groupIDs []int64
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var g groupWithFiles
			if err := rows.Scan(&g.ID, &g.ContentHash, &g.FileSize, &g.FileCount,
				&g.ReclaimableBytes, &g.FileType, &g.Status); err != nil {
				continue
			}
			if len(g.ContentHash) > 8 {
				g.HashShort = g.ContentHash[:8]
			} else {
				g.HashShort = g.ContentHash
			}
			g.DisplayName = g.HashShort + "…" // fallback until files are loaded
			groups = append(groups, g)
			groupIDs = append(groupIDs, g.ID)
		}
	}
	if groups == nil {
		groups = []groupWithFiles{}
	}

	// Batch-load files for all groups on this page.
	if len(groupIDs) > 0 {
		placeholders := strings.Repeat("?,", len(groupIDs))
		placeholders = placeholders[:len(placeholders)-1]
		fargs := make([]interface{}, len(groupIDs))
		for i, id := range groupIDs {
			fargs[i] = id
		}
		fileRows, ferr := ps.db.QueryContext(r.Context(),
			"SELECT group_id, id, path, size, mtime, file_type FROM duplicate_files "+
				"WHERE group_id IN ("+placeholders+") ORDER BY group_id, path",
			fargs...)
		if ferr == nil {
			defer fileRows.Close()
			byGroup := make(map[int64][]groupFileItem, len(groupIDs))
			for fileRows.Next() {
				var gid int64
				var f groupFileItem
				var mtime int64
				if err := fileRows.Scan(&gid, &f.ID, &f.Path, &f.Size, &mtime, &f.FileType); err != nil {
					continue
				}
				f.MTime = time.Unix(mtime, 0).Format("2006-01-02 15:04")
				byGroup[gid] = append(byGroup[gid], f)
			}
			for i, g := range groups {
				files := byGroup[g.ID]
				groups[i].Files = files
				if len(files) > 0 {
					groups[i].DisplayName = filepath.Base(files[0].Path)
				}
			}
		}
	}

	hasNext := offset+pageLimit < total
	hasPrev := offset > 0
	nextOffset := offset + pageLimit
	prevOffset := offset - pageLimit
	if prevOffset < 0 {
		prevOffset = 0
	}

	ps.renderTemplate(w, "groups.html", groupsPageData{
		baseData:          flashFromQuery(r),
		Groups:            groups,
		Total:             total,
		Limit:             pageLimit,
		Offset:            offset,
		StatusFilter:      statusFilter,
		TypeFilter:        typeFilter,
		NextOffset:        nextOffset,
		PrevOffset:        prevOffset,
		HasNext:           hasNext,
		HasPrev:           hasPrev,
		TotalActiveGroups: totalActiveGroups,
		TotalActiveFiles:  totalActiveFiles,
		TotalReclaimable:  totalReclaimable,
	})
}

func (ps *pageServer) groupDetailPage(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	var g groupPageItem
	err = ps.db.QueryRowContext(r.Context(), `
		SELECT id, content_hash, file_size, file_count, reclaimable_bytes, file_type, status
		FROM duplicate_groups WHERE id = ?`, id,
	).Scan(&g.ID, &g.ContentHash, &g.FileSize, &g.FileCount, &g.ReclaimableBytes, &g.FileType, &g.Status)
	if err == sql.ErrNoRows {
		ps.renderTemplate(w, "group_detail.html", groupDetailData{NotFound: true})
		return
	}
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	g.HashShort = g.ContentHash
	if len(g.ContentHash) > 8 {
		g.HashShort = g.ContentHash[:8]
	}

	fileRows, err := ps.db.QueryContext(r.Context(), `
		SELECT id, path, size, mtime, file_type
		FROM duplicate_files WHERE group_id = ? ORDER BY path`, id)
	var files []groupFileItem
	if err == nil {
		defer fileRows.Close()
		for fileRows.Next() {
			var f groupFileItem
			var mtime int64
			if err := fileRows.Scan(&f.ID, &f.Path, &f.Size, &mtime, &f.FileType); err != nil {
				continue
			}
			f.MTime = time.Unix(mtime, 0).Format("2006-01-02 15:04")
			files = append(files, f)
		}
	}
	if files == nil {
		files = []groupFileItem{}
	}
	if len(files) > 0 {
		g.DisplayName = filepath.Base(files[0].Path)
	} else {
		g.DisplayName = g.HashShort + "…"
	}

	ps.renderTemplate(w, "group_detail.html", groupDetailData{
		baseData: flashFromQuery(r),
		Group:    g,
		Files:    files,
	})
}

func (ps *pageServer) trashPage(w http.ResponseWriter, r *http.Request) {
	const pageLimit = 50
	offset := 0
	if v, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && v >= 0 {
		offset = v
	}

	var total int
	var totalSize int64
	ps.db.QueryRowContext(r.Context(),
		`SELECT COUNT(*), COALESCE(SUM(file_size),0) FROM trash WHERE status='trashed'`,
	).Scan(&total, &totalSize)

	rows, err := ps.db.QueryContext(r.Context(), `
		SELECT id, original_path, file_size, trashed_at, expires_at, group_id
		FROM trash WHERE status='trashed'
		ORDER BY trashed_at DESC
		LIMIT ? OFFSET ?`, pageLimit, offset)

	var items []trashPageItem
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var it trashPageItem
			var trashedAt, expiresAt int64
			var groupID sql.NullInt64
			if err := rows.Scan(&it.ID, &it.OriginalPath, &it.FileSize,
				&trashedAt, &expiresAt, &groupID); err != nil {
				continue
			}
			it.TrashedAt = time.Unix(trashedAt, 0).Format("2006-01-02 15:04")
			it.DaysRemaining = int(time.Until(time.Unix(expiresAt, 0)).Hours() / 24)
			if it.DaysRemaining < 0 {
				it.DaysRemaining = 0
			}
			if groupID.Valid {
				it.GroupID = &groupID.Int64
			}
			items = append(items, it)
		}
	}
	if items == nil {
		items = []trashPageItem{}
	}

	hasNext := offset+pageLimit < total
	hasPrev := offset > 0
	nextOffset := offset + pageLimit
	prevOffset := offset - pageLimit
	if prevOffset < 0 {
		prevOffset = 0
	}

	ps.renderTemplate(w, "trash.html", trashPageData{
		baseData:   flashFromQuery(r),
		Items:      items,
		Total:      total,
		TotalSize:  totalSize,
		Limit:      pageLimit,
		Offset:     offset,
		NextOffset: nextOffset,
		PrevOffset: prevOffset,
		HasNext:    hasNext,
		HasPrev:    hasPrev,
	})
}

// ── Fragment handlers ─────────────────────────────────────────────────────────

func (ps *pageServer) scanStatusFragment(w http.ResponseWriter, r *http.Request) {
	data := scanStatusData{}

	if ps.mgr != nil {
		if active := ps.mgr.ActiveScan(); active != nil {
			data.ScanRunning = true
			data.ScanID = active.ID
			data.StartedAt = active.StartedAt.Format("15:04:05")
			data.TriggeredBy = active.TriggeredBy
			p := active.Progress
			data.FilesDiscovered = p.FilesDiscovered.Load()
			data.CandidatesFound = p.CandidatesFound.Load()
			data.PartialHashed = p.PartialHashed.Load()
			data.FullHashed = p.FullHashed.Load()
			data.BytesRead = p.BytesRead.Load()
		}
	}

	var lastFinishedAt int64
	err := ps.db.QueryRowContext(r.Context(), `
		SELECT finished_at, duplicate_groups, reclaimable_bytes
		FROM scan_history WHERE status='completed'
		ORDER BY finished_at DESC LIMIT 1`,
	).Scan(&lastFinishedAt, &data.LastGroups, &data.LastReclaimable)
	if err == nil {
		data.HasLastScan = true
		data.LastFinishedAt = time.Unix(lastFinishedAt, 0).Format("Jan 2, 2006 15:04")
	}

	if ps.sched != nil {
		data.CronExpr = ps.sched.CronExpr()
		if t := ps.sched.NextRunAt(); t != nil {
			data.NextRunAt = t.Format("Jan 2, 2006 15:04")
		}
	}

	ps.renderFragment(w, "scan_status.html", "scan_status", data)
}

// ── UI action handlers ────────────────────────────────────────────────────────

func (ps *pageServer) uiScanStart(w http.ResponseWriter, r *http.Request) {
	if ps.mgr == nil {
		uiRedirect(w, r, "/", "error", "Scanner not available")
		return
	}
	_, err := ps.mgr.Start(context.Background(), "manual")
	if err != nil {
		if err == scan.ErrAlreadyRunning {
			uiRedirect(w, r, "/", "error", "A scan is already running")
			return
		}
		uiRedirect(w, r, "/", "error", "Failed to start scan: "+err.Error())
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (ps *pageServer) uiScanCancel(w http.ResponseWriter, r *http.Request) {
	if ps.mgr == nil {
		uiRedirect(w, r, "/", "error", "Scanner not available")
		return
	}
	if _, err := ps.mgr.Cancel(); err != nil {
		uiRedirect(w, r, "/", "error", "No scan running")
		return
	}
	uiRedirect(w, r, "/", "success", "Scan cancelled")
}

func (ps *pageServer) uiGroupDelete(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	groupID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	r.ParseForm()
	keeperIDStr := r.FormValue("keeper_id")

	var contentHash string
	var fileSize int64
	err = ps.db.QueryRowContext(r.Context(),
		`SELECT content_hash, file_size FROM duplicate_groups WHERE id = ?`, groupID,
	).Scan(&contentHash, &fileSize)
	if err != nil {
		uiRedirect(w, r, "/groups-ui", "error", "Group not found")
		return
	}

	type fileRecord struct {
		ID    int64
		Path  string
		Size  int64
		MTime int64
	}
	fileRows, err := ps.db.QueryContext(r.Context(),
		`SELECT id, path, size, mtime FROM duplicate_files WHERE group_id = ?`, groupID)
	if err != nil {
		uiRedirect(w, r, "/groups-ui", "error", "Database error")
		return
	}
	allFiles := map[int64]fileRecord{}
	for fileRows.Next() {
		var f fileRecord
		if err := fileRows.Scan(&f.ID, &f.Path, &f.Size, &f.MTime); err == nil {
			allFiles[f.ID] = f
		}
	}
	fileRows.Close()

	// Build the delete list from whichever mode was submitted.
	// Mode A (keep-one): keeper_id is set — delete everything else.
	// Mode B (select): delete_file_ids[] contains the IDs to remove.
	var deleteIDs []int64
	keeperID := int64(-1) // -1 means no explicit keeper (select-to-delete mode)
	if keeperIDStr != "" {
		// keep-one mode: delete all except the keeper
		var parseErr error
		keeperID, parseErr = strconv.ParseInt(keeperIDStr, 10, 64)
		if parseErr != nil {
			uiRedirect(w, r, "/groups-ui/"+idStr, "error", "Invalid keeper selection")
			return
		}
		for id := range allFiles {
			if id != keeperID {
				deleteIDs = append(deleteIDs, id)
			}
		}
	} else {
		// select-to-delete mode: use the checked IDs
		for _, s := range r.Form["delete_file_ids"] {
			id, err := strconv.ParseInt(s, 10, 64)
			if err == nil {
				deleteIDs = append(deleteIDs, id)
			}
		}
	}
	if len(deleteIDs) == 0 {
		uiRedirect(w, r, "/groups-ui/"+idStr, "error", "No files selected for deletion")
		return
	}
	if len(deleteIDs) >= len(allFiles) {
		uiRedirect(w, r, "/groups-ui/"+idStr, "error", "At least one file must be kept")
		return
	}

	// Validate files on disk.
	// Build a set of IDs being deleted for O(1) lookup.
	deleteSet := make(map[int64]bool, len(deleteIDs))
	for _, id := range deleteIDs {
		deleteSet[id] = true
	}
	for fid, f := range allFiles {
		info, statErr := os.Stat(f.Path)
		if deleteSet[fid] {
			if os.IsNotExist(statErr) {
				uiRedirect(w, r, "/groups-ui/"+idStr, "error", "File missing: "+f.Path+". Please re-scan.")
				return
			}
			if statErr == nil && (info.Size() != f.Size || info.ModTime().Unix() != f.MTime) {
				uiRedirect(w, r, "/groups-ui/"+idStr, "error", "File modified: "+f.Path+". Please re-scan.")
				return
			}
		} else if keeperID == fid {
			if os.IsNotExist(statErr) {
				uiRedirect(w, r, "/groups-ui/"+idStr, "error", "Keeper missing: "+f.Path+". Please re-scan.")
				return
			}
		}
	}

	retentionDays := 30
	if ps.cfg != nil && ps.cfg.TrashRetentionDays > 0 {
		retentionDays = ps.cfg.TrashRetentionDays
	}
	for _, fileID := range deleteIDs {
		f := allFiles[fileID]
		if _, err := ps.trashMgr.MoveToTrash(r.Context(), f.Path, groupID, contentHash, retentionDays); err != nil {
			uiRedirect(w, r, "/groups-ui/"+idStr, "error", "Failed to trash: "+err.Error())
			return
		}
	}

	tx, err := ps.db.BeginTx(r.Context(), nil)
	if err != nil {
		uiRedirect(w, r, "/groups-ui", "error", "Database error")
		return
	}
	defer tx.Rollback()
	for _, fileID := range deleteIDs {
		tx.ExecContext(r.Context(), `DELETE FROM duplicate_files WHERE id = ?`, fileID)
	}
	remaining := len(allFiles) - len(deleteIDs)
	newStatus := "unresolved"
	newReclaimable := fileSize * int64(remaining-1)
	if remaining <= 1 {
		newStatus = "resolved"
		newReclaimable = 0
	}
	now := time.Now().Unix()
	if newStatus == "resolved" {
		tx.ExecContext(r.Context(), `
			UPDATE duplicate_groups SET file_count=?, reclaimable_bytes=?, status=?, resolved_at=?, updated_at=?
			WHERE id=?`, remaining, newReclaimable, newStatus, now, now, groupID)
	} else {
		tx.ExecContext(r.Context(), `
			UPDATE duplicate_groups SET file_count=?, reclaimable_bytes=?, status=?, updated_at=?
			WHERE id=?`, remaining, newReclaimable, newStatus, now, groupID)
	}
	tx.Commit()

	if newStatus == "resolved" {
		uiRedirect(w, r, "/groups-ui", "success", "Files deleted, group resolved")
	} else {
		uiRedirect(w, r, "/groups-ui/"+idStr, "success", "Files deleted")
	}
}

func (ps *pageServer) uiGroupIgnore(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	groupID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	r.ParseForm()
	ignoreType := r.FormValue("type")
	dirPath := r.FormValue("path")

	var contentHash string
	err = ps.db.QueryRowContext(r.Context(),
		`SELECT content_hash FROM duplicate_groups WHERE id = ?`, groupID,
	).Scan(&contentHash)
	if err != nil {
		uiRedirect(w, r, "/groups-ui", "error", "Group not found")
		return
	}

	now := time.Now().Unix()
	var newGroupStatus string

	switch ignoreType {
	case "hash":
		ps.db.ExecContext(r.Context(),
			`INSERT OR IGNORE INTO whitelist (type, value, added_by, added_at)
			 VALUES ('hash', ?, 'user', ?)`, contentHash, now)
		newGroupStatus = "ignored"

	case "path_pair":
		pathRows, _ := ps.db.QueryContext(r.Context(),
			`SELECT path FROM duplicate_files WHERE group_id = ? ORDER BY path`, groupID)
		var paths []string
		for pathRows.Next() {
			var p string
			if err := pathRows.Scan(&p); err == nil {
				paths = append(paths, p)
			}
		}
		pathRows.Close()
		if len(paths) < 2 {
			uiRedirect(w, r, "/groups-ui/"+idStr, "error", "Group needs at least 2 files for path watching")
			return
		}
		sort.Strings(paths)
		pathJSON, _ := json.Marshal(paths)
		ps.db.ExecContext(r.Context(),
			`INSERT OR IGNORE INTO whitelist (type, value, expected_hash, added_by, added_at)
			 VALUES ('path_pair', ?, ?, 'user', ?)`,
			string(pathJSON), contentHash, now)
		newGroupStatus = "watching"

	case "dir":
		if dirPath == "" {
			uiRedirect(w, r, "/groups-ui/"+idStr, "error", "Directory path is required for dir ignore")
			return
		}
		ps.db.ExecContext(r.Context(),
			`INSERT OR IGNORE INTO whitelist (type, value, added_by, added_at)
			 VALUES ('dir', ?, 'user', ?)`, dirPath, now)
		newGroupStatus = "ignored"

	default:
		uiRedirect(w, r, "/groups-ui/"+idStr, "error", "Invalid ignore type")
		return
	}

	ps.db.ExecContext(r.Context(), `
		UPDATE duplicate_groups SET status=?, ignored_at=?, updated_at=? WHERE id=?`,
		newGroupStatus, now, now, groupID)

	uiRedirect(w, r, "/groups-ui", "success", "Group updated")
}

func (ps *pageServer) uiGroupReset(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	groupID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	now := time.Now().Unix()
	if _, err := ps.db.ExecContext(r.Context(),
		`UPDATE duplicate_groups SET status='unresolved', ignored_at=NULL, updated_at=? WHERE id=?`,
		now, groupID); err != nil {
		uiRedirect(w, r, "/groups-ui", "error", "Failed to reset group: "+err.Error())
		return
	}
	uiRedirect(w, r, "/groups-ui", "success", "Group reset to unresolved")
}

func (ps *pageServer) uiTrashRestore(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := ps.trashMgr.Restore(r.Context(), id); err != nil {
		uiRedirect(w, r, "/trash-ui", "error", "Restore failed: "+err.Error())
		return
	}
	uiRedirect(w, r, "/trash-ui", "success", "File restored")
}

func (ps *pageServer) uiTrashPurge(w http.ResponseWriter, r *http.Request) {
	count, bytesFreed, err := ps.trashMgr.PurgeAll(r.Context())
	if err != nil {
		uiRedirect(w, r, "/trash-ui", "error", "Purge failed: "+err.Error())
		return
	}
	uiRedirect(w, r, "/trash-ui", "success",
		fmt.Sprintf("Purged %d files, freed %s", count, humanBytes(bytesFreed)))
}

func (ps *pageServer) settingsPage(w http.ResponseWriter, r *http.Request) {
	d := settingsPageData{
		baseData:           flashFromQuery(r),
		ScanPaths:          strings.Join(ps.cfg.ScanPaths, "\n"),
		ExcludePaths:       strings.Join(ps.cfg.ExcludePaths, "\n"),
		Schedule:           ps.cfg.Schedule,
		ScanPaused:         ps.cfg.ScanPaused,
		TrashRetentionDays: ps.cfg.TrashRetentionDays,
		Walkers:            ps.cfg.ScanWorkers.Walkers,
		PartialHashers:     ps.cfg.ScanWorkers.PartialHashers,
		FullHashers:        ps.cfg.ScanWorkers.FullHashers,
	}
	ps.renderTemplate(w, "settings.html", d)
}

func (ps *pageServer) uiSettingsSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		uiRedirect(w, r, "/settings-ui", "error", "Invalid form data")
		return
	}

	parsePaths := func(raw string) []string {
		var out []string
		for _, line := range strings.Split(raw, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				out = append(out, line)
			}
		}
		return out
	}

	scanPaths := parsePaths(r.FormValue("scan_paths"))
	excludePaths := parsePaths(r.FormValue("exclude_paths"))
	schedule := strings.TrimSpace(r.FormValue("schedule"))
	scanPaused := r.FormValue("scan_paused") == "on"

	retention, err := strconv.Atoi(r.FormValue("trash_retention_days"))
	if err != nil || retention < 1 || retention > 365 {
		uiRedirect(w, r, "/settings-ui", "error", "Trash retention must be 1–365 days")
		return
	}
	walkers, err := strconv.Atoi(r.FormValue("walkers"))
	if err != nil || walkers < 1 {
		uiRedirect(w, r, "/settings-ui", "error", "Walkers must be at least 1")
		return
	}
	partialHashers, err := strconv.Atoi(r.FormValue("partial_hashers"))
	if err != nil || partialHashers < 1 {
		uiRedirect(w, r, "/settings-ui", "error", "Partial hashers must be at least 1")
		return
	}
	fullHashers, err := strconv.Atoi(r.FormValue("full_hashers"))
	if err != nil || fullHashers < 1 {
		uiRedirect(w, r, "/settings-ui", "error", "Full hashers must be at least 1")
		return
	}

	patch := handlers.ConfigPatch{
		ScanPaths:          scanPaths,
		ExcludePaths:       excludePaths,
		Schedule:           &schedule,
		ScanPaused:         &scanPaused,
		TrashRetentionDays: &retention,
		ScanWorkers: &handlers.WorkerPatch{
			Walkers:        &walkers,
			PartialHashers: &partialHashers,
			FullHashers:    &fullHashers,
		},
	}
	if err := ps.cfgH.Apply(r.Context(), patch); err != nil {
		uiRedirect(w, r, "/settings-ui", "error", err.Error())
		return
	}

	if ps.sched != nil && schedule != "" {
		if err := ps.sched.SetJob(schedule, func() {
			slog.Info("scheduled scan triggered")
			if _, err := ps.mgr.Start(context.Background(), "schedule"); err != nil {
				slog.Warn("scheduled scan start", "error", err)
			}
		}); err != nil {
			uiRedirect(w, r, "/settings-ui", "error", "Invalid cron expression: "+err.Error())
			return
		}
	}

	uiRedirect(w, r, "/settings-ui", "success", "Settings saved")
}
