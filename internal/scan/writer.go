package scan

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/eargollo/ditto/internal/media"
)

// WriteStats holds the final counts returned by RunDBWriter.
type WriteStats struct {
	DuplicateGroups  int64
	DuplicateFiles   int64
	ReclaimableBytes int64
	FilesHashed      int64 // files that went through full hashing (not cache hits)
}

// RunDBWriter collects all HashedFile results from in, then writes duplicate
// groups and files to the database in batched transactions.
// It also updates the file_cache for all files that passed through the
// pipeline (enabling incremental re-scans).
// Returns aggregate stats for updating scan_history.
func RunDBWriter(ctx context.Context, db *sql.DB, scanID int64, batchSize int, in <-chan HashedFile) (WriteStats, error) {
	// Phase 1: accumulate all results into a map keyed by full hash.
	groups := make(map[string][]HashedFile)
	for hf := range in {
		groups[hf.Hash] = append(groups[hf.Hash], hf)
	}
	if ctx.Err() != nil {
		return WriteStats{}, ctx.Err()
	}

	// Phase 2: write to the database.
	return persistGroups(ctx, db, scanID, batchSize, groups)
}

// persistGroups writes duplicate groups and files to the DB and updates the
// file_cache. Groups with fewer than 2 files are still cached but not written
// as duplicates.
func persistGroups(ctx context.Context, db *sql.DB, scanID int64, batchSize int, groups map[string][]HashedFile) (WriteStats, error) {
	var stats WriteStats
	now := time.Now().Unix()

	// Collect all files for cache update (regardless of duplicate status).
	var allFiles []HashedFile
	for _, files := range groups {
		allFiles = append(allFiles, files...)
		stats.FilesHashed += int64(len(files))
	}

	// ── Write duplicate groups ────────────────────────────────────────────
	for hash, files := range groups {
		if ctx.Err() != nil {
			return stats, ctx.Err()
		}
		if len(files) < 2 {
			continue
		}

		fileSize := files[0].Size
		fileType := string(media.Detect(files[0].Path))

		// INSERT OR IGNORE preserves existing status/ignored_at/resolved_at.
		_, err := db.ExecContext(ctx, `
			INSERT OR IGNORE INTO duplicate_groups
				(content_hash, file_size, file_type,
				 first_seen_scan_id, last_seen_scan_id,
				 created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			hash, fileSize, fileType,
			scanID, scanID, now, now)
		if err != nil {
			return stats, fmt.Errorf("insert group %s: %w", hash[:8], err)
		}

		var groupID int64
		if err := db.QueryRowContext(ctx,
			`SELECT id FROM duplicate_groups WHERE content_hash = ?`, hash,
		).Scan(&groupID); err != nil {
			return stats, fmt.Errorf("get group id %s: %w", hash[:8], err)
		}

		// Replace all files for this group (wholesale refresh per scan).
		if _, err := db.ExecContext(ctx,
			`DELETE FROM duplicate_files WHERE group_id = ?`, groupID,
		); err != nil {
			return stats, fmt.Errorf("delete old files group %d: %w", groupID, err)
		}

		// Insert new files in batched transactions.
		if err := insertFilesBatched(ctx, db, groupID, scanID, files, batchSize); err != nil {
			return stats, fmt.Errorf("insert files group %d: %w", groupID, err)
		}

		// Update group aggregate stats and last_seen.
		reclaimable := fileSize * int64(len(files)-1)
		if _, err := db.ExecContext(ctx, `
			UPDATE duplicate_groups
			SET file_count = ?, reclaimable_bytes = ?,
			    file_type = ?, last_seen_scan_id = ?, updated_at = ?
			WHERE id = ?`,
			len(files), reclaimable, fileType, scanID, now, groupID,
		); err != nil {
			return stats, fmt.Errorf("update group %d: %w", groupID, err)
		}

		stats.DuplicateGroups++
		stats.DuplicateFiles += int64(len(files))
		stats.ReclaimableBytes += reclaimable
	}

	// ── Update file_cache ─────────────────────────────────────────────────
	if err := updateCache(ctx, db, scanID, allFiles, batchSize); err != nil {
		// Non-fatal: cache update failure degrades next-scan performance but
		// doesn't corrupt duplicate results.
		slog.Warn("file_cache update failed", "error", err)
	}

	return stats, nil
}

// insertFilesBatched inserts files into duplicate_files in transactions of
// up to batchSize rows.
func insertFilesBatched(ctx context.Context, db *sql.DB, groupID, scanID int64, files []HashedFile, batchSize int) error {
	for i := 0; i < len(files); i += batchSize {
		end := min(i+batchSize, len(files))
		batch := files[i:end]

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		stmt, err := tx.PrepareContext(ctx, `
			INSERT INTO duplicate_files (group_id, scan_id, path, size, mtime, file_type)
			VALUES (?, ?, ?, ?, ?, ?)`)
		if err != nil {
			tx.Rollback()
			return err
		}
		for _, f := range batch {
			ft := string(media.Detect(f.Path))
			if _, err := stmt.ExecContext(ctx, groupID, scanID, f.Path, f.Size, f.MTime.Unix(), ft); err != nil {
				stmt.Close()
				tx.Rollback()
				return err
			}
		}
		stmt.Close()
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// updateCache upserts file_cache entries for all files that passed through
// the full hash stage.
func updateCache(ctx context.Context, db *sql.DB, scanID int64, files []HashedFile, batchSize int) error {
	now := time.Now().Unix()
	for i := 0; i < len(files); i += batchSize {
		end := min(i+batchSize, len(files))
		batch := files[i:end]

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		stmt, err := tx.PrepareContext(ctx, `
			INSERT OR REPLACE INTO file_cache (path, size, mtime, full_hash, cached_at, scan_id)
			VALUES (?, ?, ?, ?, ?, ?)`)
		if err != nil {
			tx.Rollback()
			return err
		}
		for _, f := range batch {
			if _, err := stmt.ExecContext(ctx, f.Path, f.Size, f.MTime.Unix(), f.Hash, now, scanID); err != nil {
				stmt.Close()
				tx.Rollback()
				return err
			}
		}
		stmt.Close()
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
