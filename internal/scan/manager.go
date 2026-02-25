package scan

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// ErrAlreadyRunning is returned when a scan is started while one is in progress.
var ErrAlreadyRunning = errors.New("a scan is already in progress")

// ErrNoActiveScan is returned when cancel is called with no scan running.
var ErrNoActiveScan = errors.New("no scan is currently running")

// ActiveScan holds live information about the running scan.
type ActiveScan struct {
	ID          int64
	StartedAt   time.Time
	TriggeredBy string
	Progress    *Progress
}

// Manager enforces a single-active-scan invariant and exposes start/cancel.
// It is safe for concurrent use.
type Manager struct {
	mu       sync.Mutex
	db       *sql.DB
	roots    []string
	excludes []string
	cfg      Config

	active   *ActiveScan
	cancelFn context.CancelFunc
}

// NewManager creates a Manager. parentCtx is used as the base for scan
// contexts; cancelling it cancels any running scan (e.g. on server shutdown).
func NewManager(db *sql.DB, roots, excludes []string, cfg Config) *Manager {
	return &Manager{
		db:       db,
		roots:    roots,
		excludes: excludes,
		cfg:      cfg,
	}
}

// UpdateConfig replaces the roots/excludes/cfg used for future scans.
// It does NOT affect a currently running scan.
func (m *Manager) UpdateConfig(roots, excludes []string, cfg Config) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.roots = roots
	m.excludes = excludes
	m.cfg = cfg
}

// Start launches an asynchronous scan. Returns an ActiveScan snapshot or
// ErrAlreadyRunning if a scan is already in progress.
func (m *Manager) Start(parentCtx context.Context, triggeredBy string) (*ActiveScan, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.active != nil {
		return nil, ErrAlreadyRunning
	}

	// Create the scan_history record NOW so the ID is available immediately
	// in the HTTP response, before the goroutine begins executing.
	startedAt := time.Now()
	scanID, err := insertScanRecord(m.db, startedAt, triggeredBy)
	if err != nil {
		return nil, fmt.Errorf("create scan record: %w", err)
	}

	progress := &Progress{}
	scanCtx, cancel := context.WithCancel(parentCtx)

	active := &ActiveScan{
		ID:          scanID,
		StartedAt:   startedAt,
		TriggeredBy: triggeredBy,
		Progress:    progress,
	}
	m.active = active
	m.cancelFn = cancel

	scanner := New(m.db, m.roots, m.excludes, m.cfg)

	go func() {
		if err := scanner.runScan(scanCtx, scanID, triggeredBy, startedAt, progress); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("scan run error", "error", err)
		}

		m.mu.Lock()
		m.active = nil
		m.cancelFn = nil
		m.mu.Unlock()
	}()

	return active, nil
}

// Cancel stops the currently running scan. Returns ErrNoActiveScan if idle.
func (m *Manager) Cancel() (*ActiveScan, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.active == nil {
		return nil, ErrNoActiveScan
	}

	snap := *m.active
	m.cancelFn()
	return &snap, nil
}

// ActiveScan returns a snapshot of the running scan, or nil when idle.
func (m *Manager) ActiveScan() *ActiveScan {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active == nil {
		return nil
	}
	snap := *m.active
	return &snap
}

// MarkStaleScansFailed marks any scan_history rows still in 'running' state
// as 'failed'. This should be called once at startup in case a previous
// server process crashed mid-scan.
func MarkStaleScansFailed(db *sql.DB) error {
	res, err := db.Exec(`
		UPDATE scan_history
		SET status = 'failed', finished_at = ?
		WHERE status = 'running'`,
		time.Now().Unix())
	if err != nil {
		return fmt.Errorf("mark stale scans failed: %w", err)
	}
	if n, _ := res.RowsAffected(); n > 0 {
		slog.Warn("marked stale scans as failed", "count", n)
	}
	return nil
}
