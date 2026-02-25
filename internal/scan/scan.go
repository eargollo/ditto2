package scan

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// FileInfo is a filesystem entry emitted by the walker.
type FileInfo struct {
	Path  string
	Size  int64
	MTime time.Time
}

// HashedFile is a FileInfo paired with a computed hash.
type HashedFile struct {
	FileInfo
	Hash string
}

// Config holds pipeline concurrency tuning parameters.
type Config struct {
	Walkers        int
	PartialHashers int
	FullHashers    int
	BatchSize      int
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		Walkers:        4,
		PartialHashers: 4,
		FullHashers:    2,
		BatchSize:      1000,
	}
}

// Scanner orchestrates the full duplicate-detection pipeline.
type Scanner struct {
	db           *sql.DB
	roots        []string
	excludePaths []string
	cfg          Config
}

// New creates a Scanner.
func New(db *sql.DB, roots, excludePaths []string, cfg Config) *Scanner {
	return &Scanner{db: db, roots: roots, excludePaths: excludePaths, cfg: cfg}
}

// runScan is called by Manager after the scan_history record has already been
// created. startedAt matches the record's started_at so duration is accurate.
func (s *Scanner) runScan(ctx context.Context, scanID int64, triggeredBy string, startedAt time.Time, progress *Progress) error {
	return s.execute(ctx, scanID, triggeredBy, startedAt, progress)
}

// Run is the standalone entry point: creates a scan_history row, executes the
// pipeline, and returns the row ID. Intended for direct use in tests.
func (s *Scanner) Run(ctx context.Context, triggeredBy string, progress *Progress) (int64, error) {
	startedAt := time.Now()
	scanID, err := insertScanRecord(s.db, startedAt, triggeredBy)
	if err != nil {
		return 0, fmt.Errorf("create scan record: %w", err)
	}
	return scanID, s.execute(ctx, scanID, triggeredBy, startedAt, progress)
}

// execute runs the pipeline for an already-created scan record.
func (s *Scanner) execute(ctx context.Context, scanID int64, triggeredBy string, startedAt time.Time, progress *Progress) error {
	slog.Info("scan started", "id", scanID, "triggered_by", triggeredBy)

	runErr := s.runPipeline(ctx, scanID, progress)

	// Determine final status.
	status := "completed"
	if ctx.Err() != nil {
		status = "cancelled"
		if runErr == nil {
			runErr = ctx.Err()
		}
	} else if runErr != nil {
		status = "failed"
	}

	finishedAt := time.Now()
	duration := int64(finishedAt.Sub(startedAt).Seconds())

	if finalErr := finaliseScanRecord(s.db, scanID, status, finishedAt.Unix(), duration, progress); finalErr != nil {
		slog.Error("finalise scan record", "id", scanID, "error", finalErr)
	}

	if status == "completed" {
		if err := insertScanSnapshot(s.db, scanID, finishedAt.Unix(), progress); err != nil {
			slog.Error("insert scan snapshot", "id", scanID, "error", err)
		}
	}

	slog.Info("scan finished", "id", scanID, "status", status,
		"files_discovered", progress.FilesDiscovered.Load())

	return runErr
}

// runPipeline wires all pipeline stages and blocks until the DB writer
// finishes or ctx is cancelled.
func (s *Scanner) runPipeline(ctx context.Context, scanID int64, progress *Progress) error {
	excludes := make(map[string]struct{}, len(s.excludePaths))
	for _, p := range s.excludePaths {
		excludes[p] = struct{}{}
	}

	const bufSize = 1000
	walkOut     := make(chan FileInfo, bufSize)
	candidates  := make(chan FileInfo, bufSize)
	cacheHits   := make(chan HashedFile, bufSize)
	cacheMisses := make(chan FileInfo, bufSize)
	partialOut  := make(chan HashedFile, bufSize)
	filteredOut := make(chan HashedFile, bufSize)
	fullOut     := make(chan HashedFile, bufSize)
	finalOut    := make(chan HashedFile, bufSize)

	// Start pipeline stages (each manages its own goroutine(s)).
	go Walk(ctx, s.roots, excludes, s.cfg.Walkers, walkOut)
	RunSizeAccumulator(ctx, progress, walkOut, candidates)
	RunCacheCheck(ctx, s.db, progress, candidates, cacheHits, cacheMisses)
	RunPartialHashers(ctx, s.cfg.PartialHashers, progress, cacheMisses, partialOut)
	RunPartialHashGrouper(ctx, partialOut, filteredOut)
	RunFullHashers(ctx, s.cfg.FullHashers, progress, filteredOut, fullOut)
	mergeHashedFiles(ctx, cacheHits, fullOut, finalOut)

	// Progress reporter — flushes counters to DB every second.
	reporterStop := make(chan struct{})
	go progressReporter(ctx, s.db, scanID, progress, reporterStop)
	defer close(reporterStop)

	stats, err := RunDBWriter(ctx, s.db, scanID, s.cfg.BatchSize, finalOut)
	if err != nil {
		return err
	}

	// Store final aggregate stats back into progress so finaliseScanRecord
	// can write them.
	progress.FullHashed.Store(stats.FilesHashed)
	// Update duplicate counters via a dedicated field — reuse CandidatesFound
	// temporarily to carry group count (written to DB by finaliseScanRecord).
	_ = stats // finaliseScanRecord queries the DB for final counts

	return nil
}

// mergeHashedFiles fans in two HashedFile channels into one. out is closed
// when both inputs are closed.
func mergeHashedFiles(ctx context.Context, a, b <-chan HashedFile, out chan<- HashedFile) {
	var wg sync.WaitGroup
	forward := func(in <-chan HashedFile) {
		defer wg.Done()
		for {
			select {
			case hf, ok := <-in:
				if !ok {
					return
				}
				select {
				case out <- hf:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}
	wg.Add(2)
	go forward(a)
	go forward(b)
	go func() {
		wg.Wait()
		close(out)
	}()
}

// progressReporter writes the current progress counters to scan_history every
// second until reporterStop is closed.
func progressReporter(ctx context.Context, db *sql.DB, scanID int64, p *Progress, stop <-chan struct{}) {
	flush := func() {
		_, err := db.ExecContext(ctx, `
			UPDATE scan_history
			SET files_discovered        = ?,
			    progress_candidates_found = ?,
			    progress_partial_hashed  = ?,
			    progress_full_hashed     = ?,
			    progress_bytes_read      = ?,
			    cache_hits               = ?,
			    cache_misses             = ?
			WHERE id = ?`,
			p.FilesDiscovered.Load(),
			p.CandidatesFound.Load(),
			p.PartialHashed.Load(),
			p.FullHashed.Load(),
			p.BytesRead.Load(),
			p.CacheHits.Load(),
			p.CacheMisses.Load(),
			scanID)
		if err != nil && ctx.Err() == nil {
			slog.Warn("progress reporter: update failed", "error", err)
		}
	}

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			flush()
		case <-stop:
			flush() // final flush
			return
		case <-ctx.Done():
			return
		}
	}
}

// ── DB helpers ────────────────────────────────────────────────────────────────

func insertScanRecord(db *sql.DB, startedAt time.Time, triggeredBy string) (int64, error) {
	now := startedAt.Unix()
	res, err := db.Exec(`
		INSERT INTO scan_history
			(started_at, status, triggered_by, created_at)
		VALUES (?, 'running', ?, ?)`,
		now, triggeredBy, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func finaliseScanRecord(db *sql.DB, scanID int64, status string, finishedAt, durationSecs int64, p *Progress) error {
	// Query final duplicate counts from the DB (written by the DB writer).
	var dupGroups, dupFiles, reclaimable int64
	_ = db.QueryRow(`
		SELECT COALESCE(SUM(1),0), COALESCE(SUM(file_count),0), COALESCE(SUM(reclaimable_bytes),0)
		FROM duplicate_groups
		WHERE last_seen_scan_id = ?`, scanID,
	).Scan(&dupGroups, &dupFiles, &reclaimable)

	_, err := db.Exec(`
		UPDATE scan_history
		SET status            = ?,
		    finished_at       = ?,
		    duration_seconds  = ?,
		    files_discovered  = ?,
		    files_hashed      = ?,
		    cache_hits        = ?,
		    cache_misses      = ?,
		    duplicate_groups  = ?,
		    duplicate_files   = ?,
		    reclaimable_bytes = ?
		WHERE id = ?`,
		status, finishedAt, durationSecs,
		p.FilesDiscovered.Load(),
		p.FullHashed.Load(),
		p.CacheHits.Load(),
		p.CacheMisses.Load(),
		dupGroups, dupFiles, reclaimable,
		scanID)
	return err
}

func insertScanSnapshot(db *sql.DB, scanID, snapshotAt int64, p *Progress) error {
	var dupGroups, dupFiles, reclaimable int64
	_ = db.QueryRow(`
		SELECT COALESCE(SUM(1),0), COALESCE(SUM(file_count),0), COALESCE(SUM(reclaimable_bytes),0)
		FROM duplicate_groups WHERE last_seen_scan_id = ?`, scanID,
	).Scan(&dupGroups, &dupFiles, &reclaimable)

	var cumDeleted, cumReclaimed int64
	_ = db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(file_size),0) FROM deletion_log`).
		Scan(&cumDeleted, &cumReclaimed)

	_, err := db.Exec(`
		INSERT INTO scan_snapshots
			(scan_id, snapshot_at,
			 duplicate_groups, duplicate_files, reclaimable_bytes,
			 cumulative_deleted_files, cumulative_reclaimed_bytes)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		scanID, snapshotAt,
		dupGroups, dupFiles, reclaimable,
		cumDeleted, cumReclaimed)
	return err
}
